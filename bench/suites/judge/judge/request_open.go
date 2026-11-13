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
	RequestOpen       = "request_open@3"
	RequestOpenBefore = "request_open@2"
	requestOpenName   = "open"
)

type Case struct {
	Kind    string `json:"kind"`
	Said    string `json:"said"`
	Replied string `json:"replied"`
	Task    string `json:"task"`
	Open    bool   `json:"open"`
	Shape   string `json:"shape"`
}

func Cases(path string) ([]Case, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cases []Case
	for at, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var one Case
		if err := json.Unmarshal([]byte(line), &one); err != nil || one.Said == "" || one.Kind == "" {
			return nil, fmt.Errorf("%s:%d is not a case with kind and said: %v", path, at+1, err)
		}
		cases = append(cases, one)
	}
	return cases, nil
}

type OpenQuestion struct {
	Ref       string
	Asked     core.Question
	Threshold float64
	State     []string
}

func OpenQuestionIn(ref string, layers []questions.Layer) (OpenQuestion, error) {
	set, fields, err := questions.Resolve(ref, layers)
	if err != nil {
		return OpenQuestion{}, err
	}
	asked, found := set.Question(requestOpenName)
	at := slices.IndexFunc(fields, func(f questions.Field) bool { return f.Path == "thresholds."+requestOpenName+"_at" })
	if !found || at < 0 {
		return OpenQuestion{}, fmt.Errorf("%s names no %s question or no thresholds.%s_at", ref, requestOpenName, requestOpenName)
	}
	threshold, err := strconv.ParseFloat(fields[at].Value, 64)
	return OpenQuestion{Ref: ref, Asked: asked.ToJev(), Threshold: threshold, State: set.State}, err
}

type Judged struct {
	Build   string
	Score   float64
	Latency time.Duration
	Cost    float64
}

func (q OpenQuestion) Ask(ctx context.Context, client *core.Client, c Case) (Judged, error) {
	fields := map[string]string{"said": c.Said, "replied": c.Replied, "task": c.Task}
	declared := map[string]string{}
	for _, name := range q.State {
		value, found := fields[name]
		if !found {
			return Judged{}, fmt.Errorf("%s reads state.%s and a case carries no such field", q.Ref, name)
		}
		declared[name] = value
	}
	state, err := json.Marshal(declared)
	if err != nil {
		return Judged{}, err
	}
	decision, err := client.Ask(ctx, core.Request{State: state, Questions: []core.Question{q.Asked}})
	if err != nil {
		return Judged{}, err
	}
	answer, found := decision.Answers[q.Asked.ID]
	if !found {
		return Judged{}, fmt.Errorf("the response carries no %s answer", q.Asked.ID)
	}
	return Judged{Build: decision.Build, Score: answer.Noul, Latency: decision.Latency, Cost: decision.Usage.Cost}, nil
}
