package ask

import (
	"context"
	"encoding/json"
	"time"

	"tofu/internal/judge/jev"
	"tofu/internal/judge/question"
	"tofu/internal/subagent"
)

const Point = "ask@1"

type Judged struct {
	Action     string
	Determined float64
	Latency    time.Duration
	Cost       float64
}

func Ask(ctx context.Context, client *jev.Client, set question.Set, state subagent.AskState) (Judged, error) {
	questions := make([]jev.Question, len(set.Questions))
	for i, q := range set.Questions {
		questions[i] = q.ToJev()
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return Judged{}, err
	}
	decision, err := client.Ask(ctx, jev.Request{State: json.RawMessage(raw), Questions: questions})
	if err != nil {
		return Judged{}, err
	}
	return Judged{
		Action:     decision.Answers[subagent.ActionQuestion].Choice,
		Determined: decision.Answers[subagent.DeterminedQuestion].Noul,
		Latency:    decision.Latency,
		Cost:       decision.Usage.Cost,
	}, nil
}

func Free(state subagent.AskState) string {
	if !state.InOwns {
		return subagent.ActionAskNow
	}
	return subagent.ActionProceed
}
