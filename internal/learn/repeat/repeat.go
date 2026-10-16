package repeat

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"tofu/internal/judge/jev"
	"tofu/internal/judge/jev/wire/typesafe"
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/question"
	"tofu/internal/konst"
	"tofu/internal/sys"
	"tofu/internal/transport"
	"tofu/library/questions"
)

const (
	Point          = "person_repeat"
	pointFile      = Point + "@1.yaml"
	standingName   = "standing"
	repeatsName    = "repeats"
	stateBuilder   = "person_repeat_state@1"
	candidateBytes = 600
	saidBytes      = 2000
)

type Earlier struct {
	Text string `json:"text"`
	At   string `json:"at,omitempty"`
}

type Rubric struct {
	Instructions string `json:"instructions"`
	True         string `json:"true"`
	False        string `json:"false"`
}

type State struct {
	Said    string    `json:"said"`
	Earlier []Earlier `json:"earlier"`
	Rubric  Rubric    `json:"rubric"`
}

type Thresholds struct {
	Standing float64
	Repeats  float64
}

type Judge struct {
	client     *jev.Client
	set        question.Set
	standing   jev.Question
	rubric     Rubric
	thresholds Thresholds
	ledger     *ledger.Writer
}

func New(project, log string) (*Judge, error) {
	layers, err := question.Layers(questions.Files(), project)
	if err != nil {
		return nil, err
	}
	set, _, err := question.Resolve(Point+"@1", layers)
	if err != nil {
		return nil, err
	}
	standing, hasStanding := set.Question(standingName)
	repeats, hasRepeats := set.Question(repeatsName)
	if !hasStanding || !hasRepeats {
		return nil, fmt.Errorf("%s names no %s or no %s question", pointFile, standingName, repeatsName)
	}
	var thresholds Thresholds
	if thresholds.Standing, err = thresholdIn(questions.Files(), standingName+"_at:"); err != nil {
		return nil, err
	}
	if thresholds.Repeats, err = thresholdIn(questions.Files(), repeatsName+"_at:"); err != nil {
		return nil, err
	}
	key, err := jev.KeyFor(sys.CredentialFileName, jev.TypeSafeVariable)
	if err != nil {
		return nil, err
	}
	wire, err := typesafe.New(typesafe.Config{Key: key, Transport: transport.Config{
		AttemptTimeout: konst.JudgeTimeoutMillis * time.Millisecond,
		Retries:        konst.JudgeRetries,
		Backoff:        konst.JudgeBackoffMillis * time.Millisecond,
		Concurrency:    1,
	}})
	if err != nil {
		return nil, err
	}
	client, err := jev.NewClient(jev.Config{Wire: wire})
	if err != nil {
		return nil, err
	}
	rubric := repeats.ToJev()
	return &Judge{client: client, set: set, standing: standing.ToJev(), rubric: Rubric{Instructions: rubric.Instructions, True: rubric.True, False: rubric.False},
		thresholds: thresholds, ledger: ledger.NewWriter(log)}, nil
}

func thresholdIn(files fs.FS, key string) (float64, error) {
	raw, err := fs.ReadFile(files, pointFile)
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if value, found := strings.CutPrefix(strings.TrimSpace(line), key); found {
			threshold, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
			if err != nil || threshold <= 0 || threshold > 1 {
				return 0, fmt.Errorf("%s: %s is a number above 0 and at or below 1, found %q", pointFile, key, value)
			}
			return threshold, nil
		}
	}
	return 0, fmt.Errorf("%s names no %s", pointFile, key)
}

func Decide(standing float64, scores []float64, thresholds Thresholds) (int, float64) {
	best, value := -1, 0.0
	if standing < thresholds.Standing {
		return best, value
	}
	for i, score := range scores {
		if score >= thresholds.Repeats && score > value {
			best, value = i, score
		}
	}
	return best, value
}

func (j *Judge) Repeats(ctx context.Context, said string, earlier []Earlier) (int, error) {
	state := State{Said: cut(said, saidBytes), Rubric: j.rubric}
	for _, one := range earlier {
		state.Earlier = append(state.Earlier, Earlier{Text: cut(one.Text, candidateBytes), At: one.At})
	}
	request := j.request(state)
	model, caps := j.client.Model(), j.client.Caps()
	for len(state.Earlier) > 1 {
		body, err := request.Encode(model)
		if err != nil {
			return -1, err
		}
		if len(body) <= caps.MaxRequestBytes && jev.EstimateTokens(len(body)) <= caps.MaxRequestTokens {
			break
		}
		state.Earlier = state.Earlier[:len(state.Earlier)-1]
		request = j.request(state)
	}
	body, err := json.Marshal(state)
	if err != nil {
		return -1, err
	}
	row := ledger.Row{Point: Point, Questions: j.set.Name, Version: j.set.QuestionsVersion, StateHash: ledger.HashOf(body), StateBuilder: stateBuilder, State: body,
		Verdict: ledger.VerdictDeny, Reason: &ledger.Reason{Comparison: ">= " + strconv.FormatFloat(j.thresholds.Repeats, 'f', -1, 64), Threshold: j.thresholds.Repeats, Mode: ledger.ModeEnforced}}
	decision, err := j.client.Ask(ctx, request)
	if err != nil {
		failed := "jev could not answer, so no note is given: " + err.Error()
		row.Verdict, row.Reason.ModeReason = ledger.VerdictAsk, &failed
		_, unwritten := j.ledger.Append(row)
		return -1, errors.Join(err, unwritten)
	}
	row.Build, row.Model, row.RequestID = decision.Build, decision.Alias, cmp.Or(decision.RequestID, decision.TransportID)
	row.LatencyMS, row.Cost = decision.Latency.Milliseconds(), decision.Usage.Cost
	scores := make([]float64, len(state.Earlier))
	for i, asked := range request.Questions {
		answer := decision.Answers[asked.ID].Noul
		row.Answers = append(row.Answers, ledger.Answer{Question: asked.ID, Wording: j.set.QuestionsVersion, Kind: ledger.AnswerNoul, Noul: answer})
		if i > 0 {
			scores[i-1] = answer
		}
	}
	best, value := Decide(row.Answers[0].Noul, scores, j.thresholds)
	row.Reason.Value = value
	if best >= 0 {
		row.Verdict, row.Reason.Question = ledger.VerdictAllow, request.Questions[best+1].ID
	}
	_, err = j.ledger.Append(row)
	return best, err
}

func (j *Judge) request(state State) jev.Request {
	questions := []jev.Question{j.standing}
	for i := range state.Earlier {
		earlier := "earlier[" + strconv.Itoa(i) + "]"
		questions = append(questions, jev.Question{
			ID:           repeatsName + "_" + strconv.Itoa(i),
			Kind:         jev.QuestionNoul,
			Instructions: "Apply the rubric in the state to said and " + earlier + ", and to no other earlier message.",
			True:         "rubric.true holds for said and " + earlier + ".",
			False:        "rubric.false holds for said and " + earlier + ".",
		})
	}
	return jev.Request{State: state, Questions: questions}
}

func cut(text string, most int) string {
	if len(text) <= most {
		return text
	}
	for !utf8.RuneStart(text[most]) {
		most--
	}
	return text[:most]
}
