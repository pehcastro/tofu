package judge

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
	RequestOpen     = "request_open@1"
	requestOpenName = "open"
)

type Case struct {
	Said  string `json:"said"`
	Open  bool   `json:"open"`
	Shape string `json:"shape"`
}

func Cases(path string) ([]Case, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cases []Case
	for at, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var one Case
		if err := json.Unmarshal([]byte(line), &one); err != nil || one.Said == "" {
			return nil, fmt.Errorf("%s:%d is not a case with said: %v", path, at+1, err)
		}
		cases = append(cases, one)
	}
	return cases, nil
}

type OpenQuestion struct {
	Asked     jev.Question
	Threshold float64
}

func OpenQuestionIn(layers []question.Layer) (OpenQuestion, error) {
	set, fields, err := question.Resolve(RequestOpen, layers)
	if err != nil {
		return OpenQuestion{}, err
	}
	asked, found := set.Question(requestOpenName)
	at := slices.IndexFunc(fields, func(f question.Field) bool { return f.Path == "thresholds."+requestOpenName+"_at" })
	if !found || at < 0 {
		return OpenQuestion{}, fmt.Errorf("%s names no %s question or no thresholds.%s_at", RequestOpen, requestOpenName, requestOpenName)
	}
	threshold, err := strconv.ParseFloat(fields[at].Value, 64)
	return OpenQuestion{Asked: asked.ToJev(), Threshold: threshold}, err
}

type Judged struct {
	Build   string
	Score   float64
	Latency time.Duration
	Cost    float64
}

func (q OpenQuestion) Ask(ctx context.Context, client *jev.Client, said string) (Judged, error) {
	state, err := json.Marshal(struct {
		Said string `json:"said"`
	}{said})
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
