package turn

import (
	"context"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/llm/wire/anthropic"
)

type Subscription struct {
	Wire *anthropic.Wire
}

func (s Subscription) Ask(ctx context.Context, request llm.Request) (llm.Decision, error) {
	var system []string
	messages := make([]llm.Message, 0, len(request.Messages))
	for _, message := range request.Messages {
		if message.Role == llm.RoleSystem {
			system = append(system, message.Content)
			continue
		}
		messages = append(messages, message)
	}

	result, _, err := s.Wire.Ask(ctx, anthropic.Request{
		System:   system,
		Messages: messages,
		Tools:    request.Tools,
		CacheTTL: konst.SubscriptionCacheTTL,
		OnDelta:  request.OnDelta,
	})
	if err != nil {
		return llm.Decision{}, err
	}

	decision := llm.Decision{
		Build:            result.Model,
		RequestID:        result.ID,
		Outcome:          llm.OutcomeAfter(result.Stop, len(result.ToolCalls)),
		Stop:             result.StopReason,
		Content:          result.Content,
		ToolCalls:        result.ToolCalls,
		Usage:            llm.Usage{InputTokens: result.Usage.Input, OutputTokens: result.Usage.Output},
		CacheReadTokens:  result.Usage.CacheRead,
		CacheWriteTokens: result.Usage.CacheWrite,
		Warnings:         result.Warnings,
	}
	if decision.Outcome == llm.OutcomeRefusal {
		decision.Refusal = result.StopReason
	}
	return decision, nil
}
