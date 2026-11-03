package wiring

import (
	"context"

	"tofu/internal/agent/context/recall"
	"tofu/internal/agent/loop"
	"tofu/internal/models/llm/core"
)

type windowGuard struct {
	inner  turn.Model
	budget recall.Budget
	cfg    recall.Config
}

func (g windowGuard) Ask(ctx context.Context, request llm.Request) (llm.Decision, error) {
	tokens := 0
	for _, message := range request.Messages {
		tokens += g.cfg.MessageTokens(message.Content)
	}
	if err := g.budget.RefuseOverWindow(tokens); err != nil {
		return llm.Decision{}, err
	}
	return g.inner.Ask(ctx, request)
}

func Guarded(model turn.Model, budget recall.Budget) (turn.Model, error) {
	cfg, err := recall.LoadConfig()
	if err != nil {
		return nil, err
	}
	return windowGuard{inner: model, budget: budget, cfg: cfg}, nil
}
