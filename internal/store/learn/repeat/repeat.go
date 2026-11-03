package repeat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"tofu/internal/judge/jev"
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/question"
	"tofu/internal/models/classifiers"
	"tofu/internal/store/learn/words"
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
	Score   float64
	Row     string
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
	client, err := classifier.Open(project, jev.ReadKeyring(), 1)
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

func highestPassing(scores []float64, from, to int, at float64) int {
	best := -1
	for i := from; i < to; i++ {
		if scores[i] >= at && (best < 0 || scores[i] > scores[best]) {
			best = i
		}
	}
	return best
}

func (t Thresholds) deciding(scores []float64, onEarlier int) (int, float64) {
	at := func(i int) float64 {
		if i >= onEarlier {
			return t.Kept
		}
		return t.Repeats
	}
	best := highestPassing(scores, onEarlier, len(scores), t.Kept)
	if best < 0 {
		best = highestPassing(scores, 0, onEarlier, t.Repeats)
	}
	if best >= 0 {
		return best, at(best)
	}
	for i := range scores {
		if best < 0 || scores[i]-at(i) > scores[best]-at(best) {
			best = i
		}
	}
	if best < 0 {
		return best, 0
	}
	return best, at(best)
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
		Verdict: ledger.VerdictDeny, Reason: atLeast(j.thresholds.Standing)}
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
	row.Reason.Question, row.Reason.Value = request.Questions[0].ID, standing
	found := none
	if best, at := j.thresholds.deciding(scores, onEarlier); standing >= j.thresholds.Standing && best >= 0 {
		row.Reason = atLeast(at)
		row.Reason.Question, row.Reason.Value = request.Questions[best+1].ID, scores[best]
		if scores[best] >= at {
			row.Verdict = ledger.VerdictAllow
			if best >= onEarlier {
				found.Kept = best - onEarlier
			} else {
				found.Earlier = best
			}
		}
	}
	written, err := j.ledger.Append(row)
	if err != nil {
		return none, err
	}
	found.Score, found.Row = row.Reason.Value, written.ID
	return found, nil
}

func atLeast(threshold float64) *ledger.Reason {
	return &ledger.Reason{Comparison: ">= " + strconv.FormatFloat(threshold, 'f', -1, 64), Threshold: threshold, Mode: ledger.ModeEnforced}
}

func KeptNear(said string, kept []Kept, sharedStemsAtLeast int) []int {
	stems := words.Stems(said)
	shared := make([]int, len(kept))
	var near []int
	for i, one := range kept {
		if shared[i] = words.Shared(stems, words.Stems(one.Text), nil); shared[i] >= sharedStemsAtLeast {
			near = append(near, i)
		}
	}
	slices.SortStableFunc(near, func(x, y int) int { return shared[y] - shared[x] })
	return near
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
