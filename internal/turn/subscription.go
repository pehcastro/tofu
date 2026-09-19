package turn

import (
	"context"

	"boji/internal/llm"
	"boji/internal/llm/wire/anthropic"
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
	})
	if err != nil {
		return llm.Decision{}, err
	}

	decision := llm.Decision{
		Build:            result.Model,
		RequestID:        result.ID,
		Outcome:          subscriptionOutcome(result),
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

func subscriptionOutcome(result anthropic.Result) llm.Outcome {
	switch result.Stop {
	case anthropic.StopError:
		return llm.OutcomeRefusal
	case anthropic.StopLength:
		return llm.OutcomeMessage
	case anthropic.StopToolUse, anthropic.StopEnd, anthropic.StopUnknown:
		if len(result.ToolCalls) > 0 {
			return llm.OutcomeToolCalls
		}
		return llm.OutcomeMessage
	}
	panic("turn: unknown anthropic stop")
}
