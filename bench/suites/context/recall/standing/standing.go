package standing

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/internal/judge/questions"
	"tofu/internal/models/classifiers/core"
)

const (
	StandingOrder = "standing_order@1"
	standingName  = "standing"
)

type Case struct {
	Sentence string `json:"sentence"`
	Standing bool   `json:"standing"`
	Shape    string `json:"shape"`
}

func Cases(path string) ([]Case, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cases []Case
	for at, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var one Case
		if err := json.Unmarshal([]byte(line), &one); err != nil || one.Sentence == "" {
			return nil, fmt.Errorf("%s:%d is not a case with sentence: %v", path, at+1, err)
		}
		cases = append(cases, one)
	}
	return cases, nil
}

type Question struct {
	Asked     jev.Question
	Threshold float64
}

func QuestionIn(layers []question.Layer) (Question, error) {
	set, fields, err := question.Resolve(StandingOrder, layers)
	if err != nil {
		return Question{}, err
	}
	asked, found := set.Question(standingName)
	at := slices.IndexFunc(fields, func(f question.Field) bool { return f.Path == "thresholds."+standingName+"_at" })
	if !found || at < 0 {
		return Question{}, fmt.Errorf("%s names no %s question or no thresholds.%s_at", StandingOrder, standingName, standingName)
	}
	threshold, err := strconv.ParseFloat(fields[at].Value, 64)
	return Question{Asked: asked.ToJev(), Threshold: threshold}, err
}

type Judged struct {
	Build   string
	Score   float64
	Latency time.Duration
	Cost    float64
}

func (q Question) Ask(ctx context.Context, client *jev.Client, sentence string) (Judged, error) {
	state, err := json.Marshal(struct {
		Sentence string `json:"sentence"`
	}{sentence})
	if err != nil {
		return Judged{}, err
	}
	decision, err := client.Ask(ctx, jev.Request{State: state, Questions: []jev.Question{q.Asked}})
	if err != nil {
		return Judged{}, err
	}
	answer, found := decision.Answers[q.Asked.ID]
	if !found {
		return Judged{}, fmt.Errorf("the response carries no %s answer", q.Asked.ID)
	}
	return Judged{Build: decision.Build, Score: answer.Noul, Latency: decision.Latency, Cost: decision.Usage.Cost}, nil
}
