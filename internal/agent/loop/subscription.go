package loop

import (
	"cmp"
	"context"

	"tofu/internal/konst"
	"tofu/internal/models/llm/core"
	"tofu/internal/models/llm/wire/anthropic"
)

type Subscription struct {
	Wire     *anthropic.Wire
	Effort   core.Effort
	CacheTTL string
}

func (s Subscription) Ask(ctx context.Context, request core.Request) (core.Decision, error) {
	var system []string
	messages := make([]core.Message, 0, len(request.Messages))
	for _, message := range request.Messages {
		if message.Role == core.RoleSystem {
			system = append(system, message.Content)
			continue
		}
		messages = append(messages, message)
	}

	result, _, err := s.Wire.Ask(ctx, anthropic.Request{
		System:     system,
		Messages:   messages,
		Tools:      request.Tools,
		ToolChoice: request.ToolChoice,
		Effort:     s.Effort,
		CacheTTL:   cmp.Or(s.CacheTTL, konst.SubscriptionCacheTTL),
		OnDelta:    request.OnDelta,
		OnThinking: request.OnThinking,
		OnRetry:    request.OnRetry,

		OmitThinkingSummary: request.OmitThinkingSummary,
	})
	if err != nil {
		return core.Decision{}, err
	}

	decision := core.Decision{
		Build:            result.Model,
		RequestID:        result.ID,
		Outcome:          core.OutcomeAfter(result.Stop, len(result.ToolCalls)),
		Stop:             result.StopReason,
		Content:          result.Content,
		Thinking:         core.Thinking{Text: result.Thinking, Signature: result.ThinkingSignature},
		ToolCalls:        result.ToolCalls,
		Usage:            core.Usage{InputTokens: result.Usage.Input, OutputTokens: result.Usage.Output, ReasoningTokens: result.Usage.Thinking},
		PromptAccounting: core.PromptAccountingFor(anthropic.Name),
		CacheReadTokens:  result.Usage.CacheRead,
		CacheWriteTokens: result.Usage.CacheWrite,
		CacheWrite5m:     result.Usage.CacheWrite5m,
		CacheWrite1h:     result.Usage.CacheWrite1h,
		FirstTokenMS:     result.FirstTokenMS,
		Warnings:         result.Warnings,
	}
	if decision.Outcome == core.OutcomeRefusal {
		decision.Refusal = result.StopReason
	}
	return decision, nil
}
