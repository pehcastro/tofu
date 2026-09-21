package llm

import (
	"encoding/json"
	"strconv"

	"tofu/internal/transport"
)

type Outcome int

const (
	OutcomeUnknown Outcome = iota
	OutcomeMessage
	OutcomeToolCalls
	OutcomeRefusal
	OutcomeTruncated
)

func (o Outcome) String() string {
	switch o {
	case OutcomeMessage:
		return "message"
	case OutcomeToolCalls:
		return "tool_calls"
	case OutcomeRefusal:
		return "refusal"
	case OutcomeTruncated:
		return "truncated"
	}
	panic("llm: unknown outcome")
}

type Usage struct {
	InputTokens  int
	OutputTokens int
	Cost         float64
}

const (
	WireAnthropic  = "anthropic"
	WireCodex      = "codex"
	WireOpenRouter = "openrouter"
)

type PromptAccounting string

const (
	PromptExcludesCacheReads PromptAccounting = "excludes_cache_reads"
	PromptIncludesCacheReads PromptAccounting = "includes_cache_reads"
)

func PromptAccountingFor(wire string) PromptAccounting {
	if wire == WireCodex || wire == WireOpenRouter {
		return PromptIncludesCacheReads
	}
	return PromptExcludesCacheReads
}

func (p PromptAccounting) FreshTokens(promptTokens, cacheReadTokens int) int {
	if p == PromptIncludesCacheReads {
		return promptTokens - cacheReadTokens
	}
	return promptTokens
}

func (p PromptAccounting) BilledTokens(promptTokens, cacheReadTokens int) int {
	if p == PromptIncludesCacheReads {
		return promptTokens
	}
	return promptTokens + cacheReadTokens
}

type Response struct {
	Build           string
	RequestID       string
	Outcome         Outcome
	Stop            string
	Content         string
	ToolCalls       []ToolCall
	Refusal         string
	Usage           Usage
	CacheReadTokens int
	Warnings        []string
	Raw             []byte
}

type wireUsage struct {
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	Cost             float64 `json:"cost"`
	PromptDetails    struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
}

type wireResponseMessage struct {
	Content   string         `json:"content"`
	Refusal   string         `json:"refusal"`
	ToolCalls []wireToolCall `json:"tool_calls"`
}

type wireChoice struct {
	FinishReason string              `json:"finish_reason"`
	Message      wireResponseMessage `json:"message"`
}

type wireError struct {
	Message string `json:"message"`
}

type wireResponse struct {
	ID      string       `json:"id"`
	Model   string       `json:"model"`
	Choices []wireChoice `json:"choices"`
	Usage   wireUsage    `json:"usage"`
	Error   *wireError   `json:"error"`
}

func Decode(raw []byte) (Response, error) {
	var wire wireResponse
	if err := json.Unmarshal(raw, &wire); err != nil {
		return Response{}, transport.Fail("llm.Decode", transport.KindInvalidAnswer, err, "the response is not the expected object")
	}
	if wire.Error != nil {
		return Response{}, transport.Fail("llm.Decode", transport.KindProvider, nil, "the provider reported an error: %s", wire.Error.Message)
	}
	if wire.Model == "" {
		return Response{}, transport.Fail("llm.Decode", transport.KindInvalidAnswer, nil, "the response reports no build id")
	}
	if len(wire.Choices) == 0 {
		return Response{}, transport.Fail("llm.Decode", transport.KindInvalidAnswer, nil, "the response returned no choices")
	}

	choice := wire.Choices[0]
	calls, err := decodeToolCalls(choice.Message.ToolCalls)
	if err != nil {
		return Response{}, err
	}

	stop, handled := MapFinishReason(choice.FinishReason)
	outcome := OutcomeAfter(stop, len(calls))
	if choice.Message.Refusal != "" && choice.Message.Content == "" {
		outcome = OutcomeRefusal
	}
	if outcome == OutcomeMessage && choice.Message.Content == "" {
		return Response{}, transport.Fail("llm.Decode", transport.KindInvalidAnswer, nil,
			"the response has no content, no tool calls and no refusal")
	}

	response := Response{
		Build:           wire.Model,
		RequestID:       wire.ID,
		Outcome:         outcome,
		Stop:            choice.FinishReason,
		Content:         choice.Message.Content,
		ToolCalls:       calls,
		Refusal:         choice.Message.Refusal,
		Usage:           Usage{InputTokens: wire.Usage.PromptTokens, OutputTokens: wire.Usage.CompletionTokens, Cost: wire.Usage.Cost},
		CacheReadTokens: wire.Usage.PromptDetails.CachedTokens,
		Raw:             raw,
	}
	if !handled {
		response.Warnings = []string{"unhandled finish reason: " + strconv.Quote(choice.FinishReason)}
	}
	return response, nil
}

func decodeToolCalls(wire []wireToolCall) ([]ToolCall, error) {
	calls := make([]ToolCall, len(wire))
	for index, call := range wire {
		if call.ID == "" {
			return nil, transport.Fail("llm.Decode", transport.KindInvalidAnswer, nil, "tool call %d has no id", index)
		}
		if call.Function.Name == "" {
			return nil, transport.Fail("llm.Decode", transport.KindInvalidAnswer, nil, "tool call %d has no function name", index)
		}
		if !json.Valid([]byte(call.Function.Arguments)) {
			return nil, transport.Fail("llm.Decode", transport.KindInvalidAnswer, nil,
				"tool call %d for %q has arguments that are not valid json", index, call.Function.Name)
		}
		calls[index] = ToolCall{ID: call.ID, Name: call.Function.Name, Arguments: json.RawMessage(call.Function.Arguments)}
	}
	return calls, nil
}
