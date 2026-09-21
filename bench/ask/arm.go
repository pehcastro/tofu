package ask

import (
	"context"
	"encoding/json"
	"time"

	"tofu/internal/crew"
	"tofu/internal/judge/jev"
	"tofu/internal/judge/jev/wire/openrouter"
	"tofu/internal/judge/question"
	"tofu/internal/konst"
	"tofu/internal/transport"
)

const Point = "ask@1"

func NewWire(key string) (*openrouter.Wire, error) {
	return openrouter.New(openrouter.Config{
		Key: key,
		Transport: transport.Config{
			AttemptTimeout: time.Duration(konst.JudgeTimeoutMillis) * time.Millisecond,
			Retries:        konst.JudgeRetries,
			Backoff:        time.Duration(konst.JudgeBackoffMillis) * time.Millisecond,
			Concurrency:    konst.SiftConcurrency,
		},
	})
}

type Judged struct {
	Action     string
	Determined float64
	Latency    time.Duration
	Cost       float64
}

func Ask(ctx context.Context, client *jev.Client, set question.Set, state crew.AskState) (Judged, error) {
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
		Action:     decision.Answers[crew.ActionQuestion].Choice,
		Determined: decision.Answers[crew.DeterminedQuestion].Noul,
		Latency:    decision.Latency,
		Cost:       decision.Usage.Cost,
	}, nil
}

func Free(state crew.AskState) string {
	if !state.InOwns {
		return crew.ActionAskNow
	}
	return crew.ActionProceed
}
