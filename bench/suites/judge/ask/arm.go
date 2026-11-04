package ask

import (
	"context"
	"encoding/json"
	"time"

	"tofu/internal/agent/subagents"
	question "tofu/internal/judge/questions"
	"tofu/internal/models/classifiers/core"
)

const Point = "ask@1"

type Judged struct {
	Action     string
	Determined float64
	Latency    time.Duration
	Cost       float64
}

func Ask(ctx context.Context, client *core.Client, set question.Set, state subagents.AskState) (Judged, error) {
	questions := make([]core.Question, len(set.Questions))
	for i, q := range set.Questions {
		questions[i] = q.ToJev()
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return Judged{}, err
	}
	decision, err := client.Ask(ctx, core.Request{State: json.RawMessage(raw), Questions: questions})
	if err != nil {
		return Judged{}, err
	}
	return Judged{
		Action:     decision.Answers[subagents.ActionQuestion].Choice,
		Determined: decision.Answers[subagents.DeterminedQuestion].Noul,
		Latency:    decision.Latency,
		Cost:       decision.Usage.Cost,
	}, nil
}

func Free(state subagents.AskState) string {
	if !state.InOwns {
		return subagents.ActionAskNow
	}
	return subagents.ActionProceed
}
