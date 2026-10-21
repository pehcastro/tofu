package repeat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"unicode/utf8"

	"tofu/internal/judge/jev"
	"tofu/internal/judge/jev/wire/typesafe"
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/question"
	"tofu/library/questions"
)

const (
	Point          = "person_repeat"
	pointFile      = Point + "@1.yaml"
	standingName   = "standing"
	repeatsName    = "repeats"
	keptName       = "kept"
	stateBuilder   = "person_repeat_state@2"
	candidateBytes = 600
	saidBytes      = 2000
)

type Earlier struct {
	Text string `json:"text"`
	At   string `json:"at,omitempty"`
}

type Kept struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

type Rubric struct {
	Instructions string `json:"instructions"`
	True         string `json:"true"`
	False        string `json:"false"`
}

type State struct {
	Said       string    `json:"said"`
	Earlier    []Earlier `json:"earlier"`
	Rubric     Rubric    `json:"rubric"`
	Kept       []Kept    `json:"kept"`
	KeptRubric Rubric    `json:"kept_rubric"`
}

type Thresholds struct {
	Standing float64
	Repeats  float64
	Kept     float64
}

type Found struct {
	Earlier int
	Kept    int
}

type Judge struct {
	client     *jev.Client
	set        question.Set
	standing   jev.Question
	rubric     Rubric
	keptRubric Rubric
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
	kept, hasKept := set.Question(keptName)
	if !hasStanding || !hasRepeats || !hasKept {
		return nil, fmt.Errorf("%s names no %s, %s or %s question", pointFile, standingName, repeatsName, keptName)
	}
	var thresholds Thresholds
	if thresholds.Standing, err = thresholdIn(questions.Files(), standingName+"_at:"); err != nil {
		return nil, err
	}
	if thresholds.Repeats, err = thresholdIn(questions.Files(), repeatsName+"_at:"); err != nil {
		return nil, err
	}
	if thresholds.Kept, err = thresholdIn(questions.Files(), keptName+"_at:"); err != nil {
		return nil, err
	}
	client, err := typesafe.Judge()
	if err != nil {
		return nil, err
	}
	return &Judge{client: client, set: set, standing: standing.ToJev(), rubric: rubricOf(repeats), keptRubric: rubricOf(kept), thresholds: thresholds, ledger: ledger.NewWriter(log)}, nil
}

func rubricOf(q question.Question) Rubric {
	asked := q.ToJev()
	return Rubric{Instructions: asked.Instructions, True: asked.True, False: asked.False}
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
	return highest(standing, scores, thresholds.Standing, thresholds.Repeats)
}

func highest(standing float64, scores []float64, standingAt, at float64) (int, float64) {
	best, value := -1, 0.0
	if standing < standingAt {
		return best, value
	}
	for i, score := range scores {
		if score >= at && score > value {
			best, value = i, score
		}
	}
	return best, value
}

func (j *Judge) Repeats(ctx context.Context, said string, earlier []Earlier, kept []Kept) (Found, error) {
	none := Found{Earlier: -1, Kept: -1}
	state := State{Said: cut(said, saidBytes), Rubric: j.rubric, Kept: []Kept{}, KeptRubric: j.keptRubric}
	for _, one := range earlier {
		state.Earlier = append(state.Earlier, Earlier{Text: cut(one.Text, candidateBytes), At: one.At})
	}
	for _, one := range kept {
		state.Kept = append(state.Kept, Kept{ID: one.ID, Text: cut(one.Text, candidateBytes)})
	}
	request := j.request(state)
	model, caps := j.client.Model(), j.client.Caps()
	for len(state.Earlier)+len(state.Kept) > 1 {
		body, err := request.Encode(model)
		if err != nil {
			return none, err
		}
		if len(body) <= caps.MaxRequestBytes && jev.EstimateTokens(len(body)) <= caps.MaxRequestTokens {
			break
		}
		if len(state.Earlier) > 0 {
			state.Earlier = state.Earlier[:len(state.Earlier)-1]
		} else {
			state.Kept = state.Kept[:len(state.Kept)-1]
		}
		request = j.request(state)
	}
	body, err := json.Marshal(state)
	if err != nil {
		return none, err
	}
	row := ledger.Row{Point: Point, Questions: j.set.Name, Version: j.set.QuestionsVersion, StateHash: ledger.HashOf(body), StateBuilder: stateBuilder, State: body,
		Verdict: ledger.VerdictDeny, Reason: &ledger.Reason{Comparison: ">= " + strconv.FormatFloat(j.thresholds.Repeats, 'f', -1, 64), Threshold: j.thresholds.Repeats, Mode: ledger.ModeEnforced}}
	decision, err := j.client.Ask(ctx, request)
	if err != nil {
		failed := "jev could not answer, so no note is given: " + err.Error()
		row.Verdict, row.Reason.ModeReason = ledger.VerdictAsk, &failed
		_, unwritten := j.ledger.Append(row)
		return none, errors.Join(err, unwritten)
	}
	decision.Record(&row, request.Questions, j.set.QuestionsVersion)
	scores := make([]float64, len(row.Answers)-1)
	for i, answer := range row.Answers[1:] {
		scores[i] = answer.Noul
	}
	standing, onEarlier := row.Answers[0].Noul, len(state.Earlier)
	found := none
	found.Kept, row.Reason.Value = highest(standing, scores[onEarlier:], j.thresholds.Standing, j.thresholds.Kept)
	asked := onEarlier + found.Kept
	if found.Kept >= 0 {
		row.Reason.Comparison, row.Reason.Threshold = ">= "+strconv.FormatFloat(j.thresholds.Kept, 'f', -1, 64), j.thresholds.Kept
	} else {
		found.Earlier, row.Reason.Value = Decide(standing, scores[:onEarlier], j.thresholds)
		asked = found.Earlier
	}
	if found != none {
		row.Verdict, row.Reason.Question = ledger.VerdictAllow, request.Questions[asked+1].ID
	}
	_, err = j.ledger.Append(row)
	return found, err
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
	for i := range state.Kept {
		kept := "kept[" + strconv.Itoa(i) + "]"
		questions = append(questions, jev.Question{
			ID:           keptName + "_" + strconv.Itoa(i),
			Kind:         jev.QuestionNoul,
			Instructions: "Apply kept_rubric in the state to said and " + kept + ", and to no other kept rule.",
			True:         "kept_rubric.true holds for said and " + kept + ".",
			False:        "kept_rubric.false holds for said and " + kept + ".",
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
