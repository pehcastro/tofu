package anthropic

import (
	"bufio"
	"encoding/json"
	"io"
	"strconv"
	"strings"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/transport"
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
	Input      int
	Output     int
	CacheRead  int
	CacheWrite int
}

type Result struct {
	ID                string
	Model             string
	Stop              Stop
	StopReason        string
	Content           string
	Thinking          string
	ThinkingSignature string
	ToolCalls         []llm.ToolCall
	Usage             Usage
	Warnings          []string
}

type streamEvent struct {
	Type    string `json:"type"`
	Index   int    `json:"index"`
	Message struct {
		ID    string    `json:"id"`
		Model string    `json:"model"`
		Usage wireUsage `json:"usage"`
	} `json:"message"`
	ContentBlock struct {
		Type      string `json:"type"`
		ID        string `json:"id"`
		Name      string `json:"name"`
		Signature string `json:"signature"`
	} `json:"content_block"`
	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		Thinking    string `json:"thinking"`
		Signature   string `json:"signature"`
		PartialJSON string `json:"partial_json"`
		StopReason  string `json:"stop_reason"`
	} `json:"delta"`
	Usage wireUsage `json:"usage"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

type wireUsage struct {
	Input      *int `json:"input_tokens"`
	Output     *int `json:"output_tokens"`
	CacheRead  *int `json:"cache_read_input_tokens"`
	CacheWrite *int `json:"cache_creation_input_tokens"`
}

func (u wireUsage) applyTo(usage *Usage) {
	if u.Input != nil {
		usage.Input = *u.Input
	}
	if u.Output != nil {
		usage.Output = *u.Output
	}
	if u.CacheRead != nil {
		usage.CacheRead = *u.CacheRead
	}
	if u.CacheWrite != nil {
		usage.CacheWrite = *u.CacheWrite
	}
}

type openBlock struct {
	kind      string
	toolIndex int
	arguments strings.Builder
}

func ReadStream(body io.Reader, oauth bool, onDelta func(string)) (Result, error) {
	var result Result
	open := map[int]*openBlock{}
	var text, thinking, signature strings.Builder
	sawStart, sawTerminal, sawStop := false, false, false

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, konst.StreamReadBytes), konst.StreamLineBytes)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue
		}
		var event streamEvent
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			return result, transport.Fail("anthropic.ReadStream", transport.KindInvalidAnswer, err,
				"an event is not the expected object")
		}

		switch event.Type {
		case "error":
			return result, transport.Fail("anthropic.ReadStream", transport.KindProvider, nil,
				"the stream carried an error: %s: %s", event.Error.Type, event.Error.Message)

		case "message_start":
			if sawStart {
				result.Warnings = append(result.Warnings, "duplicate message_start event")
				continue
			}
			sawStart = true
			result.ID, result.Model = event.Message.ID, event.Message.Model
			event.Message.Usage.applyTo(&result.Usage)

		case "content_block_start":
			if !sawStart {
				return result, transport.Fail("anthropic.ReadStream", transport.KindInvalidAnswer, nil,
					"received %s before message_start", event.Type)
			}
			block := &openBlock{kind: event.ContentBlock.Type, toolIndex: -1}
			if event.ContentBlock.Signature != "" {
				signature.WriteString(event.ContentBlock.Signature)
			}
			if event.ContentBlock.Type == "tool_use" {
				block.toolIndex = len(result.ToolCalls)
				result.ToolCalls = append(result.ToolCalls, llm.ToolCall{
					ID:   event.ContentBlock.ID,
					Name: DecodeToolName(event.ContentBlock.Name, oauth),
				})
			}
			open[event.Index] = block

		case "content_block_delta":
			block := open[event.Index]
			if block == nil {
				result.Warnings = append(result.Warnings, "a delta arrived for a block that never started")
				continue
			}
			switch event.Delta.Type {
			case "text_delta":
				text.WriteString(event.Delta.Text)
				if onDelta != nil && event.Delta.Text != "" {
					onDelta(event.Delta.Text)
				}
			case "thinking_delta":
				thinking.WriteString(event.Delta.Thinking)
			case "signature_delta":
				signature.WriteString(event.Delta.Signature)
			case "input_json_delta":
				block.arguments.WriteString(event.Delta.PartialJSON)
			}

		case "content_block_stop":
			if block := open[event.Index]; block != nil {
				closeBlock(&result, block)
				delete(open, event.Index)
			}

		case "message_delta":
			event.Usage.applyTo(&result.Usage)
			if event.Delta.StopReason == "" {
				continue
			}
			if sawTerminal {
				result.Warnings = append(result.Warnings, "a stop reason arrived after the terminal envelope")
				continue
			}
			sawTerminal = true
			result.StopReason = event.Delta.StopReason
			stop, handled := llm.MapFinishReason(event.Delta.StopReason)
			result.Stop = stop
			if !handled {
				result.Warnings = append(result.Warnings, "unhandled stop reason: "+event.Delta.StopReason)
			}

		case "message_stop":
			sawTerminal, sawStop = true, true
		}

		if sawStop {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return result, transport.Fail("anthropic.ReadStream", transport.KindProvider, err, "reading the stream")
	}

	if !sawStart {
		return result, transport.Fail("anthropic.ReadStream", transport.KindInvalidAnswer, nil,
			"stream ended before message_start")
	}
	if !sawTerminal {
		return result, transport.Fail("anthropic.ReadStream", transport.KindInvalidAnswer, nil,
			"stream ended before message_stop")
	}
	if !sawStop {
		result.Warnings = append(result.Warnings, "stream ended before message_stop")
	}
	for index, block := range open {
		result.Warnings = append(result.Warnings,
			"stream ended with an unterminated "+block.kind+" block at index "+strconv.Itoa(index))
		closeBlock(&result, block)
	}

	result.Content, result.Thinking, result.ThinkingSignature = text.String(), thinking.String(), signature.String()
	return result, nil
}

func closeBlock(result *Result, block *openBlock) {
	if block.toolIndex < 0 {
		return
	}
	arguments := block.arguments.String()
	if arguments == "" {
		arguments = "{}"
	}
	if !json.Valid([]byte(arguments)) {
		result.Warnings = append(result.Warnings,
			"tool call "+result.ToolCalls[block.toolIndex].Name+" streamed arguments that are not valid json")
		return
	}
	result.ToolCalls[block.toolIndex].Arguments = json.RawMessage(arguments)
}
