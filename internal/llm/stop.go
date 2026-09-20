package llm

import "strconv"

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
