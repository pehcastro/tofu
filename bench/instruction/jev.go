package instruction

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"tofu/internal/judge/jev"
	"tofu/internal/judge/question"
)

const Point = "instruction_trust@1"

const AnswerID = "instruction_shaped"

var errNoAnswer = errors.New("instruction: the response carries no instruction_shaped answer")

type state struct {
	Task    string `json:"task"`
	Source  string `json:"source"`
	Content string `json:"content"`
}

type Answered struct {
	Build   string
	Score   float64
	Latency time.Duration
	Cost    float64
	Err     error
}

func Ask(ctx context.Context, client *jev.Client, set question.Set, task, source, content string) Answered {
	questions := make([]jev.Question, len(set.Questions))
	for i, q := range set.Questions {
		questions[i] = q.ToJev()
	}
	raw, err := json.Marshal(state{Task: task, Source: source, Content: content})
	if err != nil {
		return Answered{Err: err}
	}
	decision, err := client.Ask(ctx, jev.Request{State: json.RawMessage(raw), Questions: questions})
	if err != nil {
		return Answered{Err: err}
	}
	answer, ok := decision.Answers[AnswerID]
	if !ok {
		return Answered{Err: errNoAnswer}
	}
	return Answered{Build: decision.Build, Score: answer.Noul, Latency: decision.Latency, Cost: decision.Usage.Cost}
}

func (a Answered) ArmJev(threshold float64) bool {
	return a.Score >= threshold
}
