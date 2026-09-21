package llm

import (
	"strconv"
	"strings"
)

type Stop int

const (
	StopUnknown Stop = iota
	StopEnd
	StopLength
	StopToolUse
	StopError
)

func (s Stop) String() string {
	switch s {
	case StopUnknown:
		return "unknown"
	case StopEnd:
		return "stop"
	case StopLength:
		return "length"
	case StopToolUse:
		return "tool_use"
	case StopError:
		return "error"
	}
	panic("llm: unknown stop " + strconv.Itoa(int(s)))
}

func MapFinishReason(reason string) (Stop, bool) {
	if strings.HasSuffix(reason, ":max_output_tokens") {
		return StopLength, true
	}
	switch reason {
	case "stop", "end_turn", "stop_sequence", "pause_turn", "compaction", "completed":
		return StopEnd, true
	case "length", "max_tokens", "model_context_window_exceeded":
		return StopLength, true
	case "tool_calls", "tool_use", "function_call":
		return StopToolUse, true
	case "content_filter", "error", "refusal", "sensitive":
		return StopError, true
	}
	return StopEnd, false
}

func OutcomeAfter(stop Stop, toolCalls int) Outcome {
	switch stop {
	case StopError:
		return OutcomeRefusal
	case StopLength:
		return OutcomeTruncated
	case StopToolUse, StopEnd, StopUnknown:
		if toolCalls > 0 {
			return OutcomeToolCalls
		}
		return OutcomeMessage
	}
	panic("llm: unknown stop " + strconv.Itoa(int(stop)))
}
