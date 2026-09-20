package codex

import (
	"bufio"
	"encoding/json"
	"io"
	"strconv"
	"strings"

	"boji/internal/llm"
	"boji/internal/transport"
)

type Stop = llm.Stop

const (
	StopUnknown = llm.StopUnknown
	StopEnd     = llm.StopEnd
	StopLength  = llm.StopLength
	StopToolUse = llm.StopToolUse
	StopError   = llm.StopError
)

type Usage struct {
	Input     int
	Output    int
	CacheRead int
	Reasoning int
	Total     int
}

type Result struct {
	ID         string
	Model      string
	Stop       Stop
	StopReason string
	Content    string
	Refusal    string
	Thinking   string
	ToolCalls  []llm.ToolCall
	Usage      Usage
	TurnState  string
	Warnings   []string
}

const (
	streamReadBytes = 64 << 10
	streamLineBytes = 16 << 20
)

type streamItem struct {
	Type      string `json:"type"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type streamEvent struct {
	Type        string     `json:"type"`
	OutputIndex int        `json:"output_index"`
	Delta       string     `json:"delta"`
	Arguments   string     `json:"arguments"`
	Item        streamItem `json:"item"`
	Response    struct {
		ID                string `json:"id"`
		Model             string `json:"model"`
		Status            string `json:"status"`
		IncompleteDetails *struct {
			Reason string `json:"reason"`
		} `json:"incomplete_details"`
		Usage *struct {
			InputTokens        int `json:"input_tokens"`
			OutputTokens       int `json:"output_tokens"`
			TotalTokens        int `json:"total_tokens"`
			InputTokensDetails struct {
				CachedTokens int `json:"cached_tokens"`
			} `json:"input_tokens_details"`
			OutputTokensDetails struct {
				ReasoningTokens int `json:"reasoning_tokens"`
			} `json:"output_tokens_details"`
		} `json:"usage"`
	} `json:"response"`
	Headers map[string]string `json:"headers"`
	Error   struct {
		Type    string `json:"type"`
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type openItem struct {
	kind      string
	toolIndex int
	arguments strings.Builder
}

type streamState struct {
	result      Result
	open        map[int]*openItem
	text        strings.Builder
	refusal     strings.Builder
	thinking    strings.Builder
	blankDeltas int
	blankBytes  int
	terminal    bool
}

func ReadStream(body io.Reader) (Result, error) {
	state := streamState{open: map[int]*openItem{}}
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, streamReadBytes), streamLineBytes)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var event streamEvent
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			return state.finish(), transport.Fail("codex.ReadStream", transport.KindInvalidAnswer, err,
				"an event is not the expected object")
		}
		if err := state.handle(event); err != nil {
			return state.finish(), err
		}
		if state.terminal {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return state.finish(), transport.Fail("codex.ReadStream", transport.KindProvider, err, "reading the stream")
	}
	if !state.terminal {
		return state.finish(), transport.Fail("codex.ReadStream", transport.KindProvider, nil,
			"the codex stream drained before a terminal event, so the turn is truncated rather than complete")
	}
	return state.finish(), nil
}

func (s *streamState) handle(event streamEvent) error {
	switch event.Type {
	case "error", "response.failed":
		return transport.Fail("codex.ReadStream", transport.KindProvider, nil,
			"the stream carried an error: %s %s: %s", event.Error.Type, event.Error.Code, event.Error.Message)

	case "response.output_item.added":
		item := &openItem{kind: event.Item.Type, toolIndex: -1}
		if event.Item.Type == "function_call" {
			item.toolIndex = len(s.result.ToolCalls)
			s.result.ToolCalls = append(s.result.ToolCalls,
				llm.ToolCall{ID: event.Item.CallID, Name: event.Item.Name})
		}
		s.open[event.OutputIndex] = item

	case "response.output_text.delta":
		s.text.WriteString(event.Delta)

	case "response.refusal.delta":
		s.refusal.WriteString(event.Delta)

	case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
		s.thinking.WriteString(event.Delta)

	case "response.reasoning_summary_part.done":
		s.thinking.WriteString("\n\n")

	case "response.function_call_arguments.delta":
		item := s.open[event.OutputIndex]
		if item == nil {
			s.result.Warnings = append(s.result.Warnings, "an argument delta arrived for an item that never started")
			return nil
		}
		item.arguments.WriteString(event.Delta)
		return s.guardWhitespaceLoop(event.Delta)

	case "response.function_call_arguments.done":
		if item := s.open[event.OutputIndex]; item != nil && event.Arguments != "" {
			item.arguments.Reset()
			item.arguments.WriteString(event.Arguments)
		}

	case "response.output_item.done":
		s.closeItem(event)

	case "response.metadata":
		if value := event.Headers[HeaderTurnState]; value != "" && s.result.TurnState == "" {
			s.result.TurnState = value
		}

	case "response.completed", "response.done", "response.incomplete":
		s.terminal = true
		s.result.ID, s.result.Model = event.Response.ID, event.Response.Model
		s.result.StopReason = event.Response.Status
		if details := event.Response.IncompleteDetails; details != nil && details.Reason != "" {
			s.result.StopReason = event.Response.Status + ":" + details.Reason
		}
		if usage := event.Response.Usage; usage != nil {
			s.result.Usage = Usage{
				Input:     usage.InputTokens,
				Output:    usage.OutputTokens,
				CacheRead: usage.InputTokensDetails.CachedTokens,
				Reasoning: usage.OutputTokensDetails.ReasoningTokens,
				Total:     usage.TotalTokens,
			}
		}
	}
	return nil
}

func (s *streamState) guardWhitespaceLoop(delta string) error {
	if strings.TrimSpace(delta) != "" {
		s.blankDeltas, s.blankBytes = 0, 0
		return nil
	}
	s.blankDeltas++
	s.blankBytes += len(delta)
	if s.blankDeltas < WhitespaceDeltaCap && s.blankBytes < WhitespaceByteCap {
		return nil
	}
	return transport.Fail("codex.ReadStream", transport.KindProvider, nil,
		"the stream emitted %d whitespace-only tool argument deltas and %d bytes without progressing",
		s.blankDeltas, s.blankBytes)
}

func (s *streamState) closeItem(event streamEvent) {
	item := s.open[event.OutputIndex]
	if item == nil {
		return
	}
	delete(s.open, event.OutputIndex)
	if item.toolIndex < 0 {
		return
	}
	call := &s.result.ToolCalls[item.toolIndex]
	if event.Item.CallID != "" {
		call.ID = event.Item.CallID
	}
	if event.Item.Name != "" {
		call.Name = event.Item.Name
	}
	arguments := item.arguments.String()
	if event.Item.Arguments != "" {
		arguments = event.Item.Arguments
	}
	if arguments == "" {
		arguments = "{}"
	}
	if !json.Valid([]byte(arguments)) {
		s.result.Warnings = append(s.result.Warnings,
			"tool call "+call.Name+" streamed arguments that are not valid json")
		return
	}
	call.Arguments = json.RawMessage(arguments)
}

func (s *streamState) finish() Result {
	s.result.Content = s.text.String()
	s.result.Thinking = strings.TrimSpace(s.thinking.String())
	s.result.Refusal = s.refusal.String()
	for index, item := range s.open {
		s.result.Warnings = append(s.result.Warnings,
			"the stream ended with an unterminated "+item.kind+" item at output index "+strconv.Itoa(index))
	}
	s.result.Stop = s.stop()
	return s.result
}

func (s *streamState) stop() Stop {
	switch {
	case !s.terminal:
		return StopUnknown
	case strings.HasSuffix(s.result.StopReason, ":max_output_tokens"):
		return StopLength
	case s.result.Refusal != "":
		return StopError
	case len(s.result.ToolCalls) > 0:
		return StopToolUse
	}
	return StopEnd
}
