package llm

import (
	"encoding/json"
	"strings"
	"time"

	"tofu/internal/transport"
)

type Role int

const (
	RoleUnknown Role = iota
	RoleSystem
	RoleUser
	RoleAssistant
	RoleTool
)

func (r Role) String() string {
	switch r {
	case RoleUnknown:
		return "unknown"
	case RoleSystem:
		return "system"
	case RoleUser:
		return "user"
	case RoleAssistant:
		return "assistant"
	case RoleTool:
		return "tool"
	}
	panic("llm: unknown role")
}

type ToolCall struct {
	ID        string
	Name      string
	Arguments json.RawMessage
}

type ToolOutcome int

const (
	ToolOutcomeUnset ToolOutcome = iota
	ToolOutcomeRan
	ToolOutcomeFailed
	ToolOutcomeAborted
)

func (o ToolOutcome) Failed() bool {
	switch o {
	case ToolOutcomeUnset, ToolOutcomeRan, ToolOutcomeAborted:
		return false
	case ToolOutcomeFailed:
		return true
	}
	panic("llm: unknown tool outcome")
}

type Image struct {
	MediaType string
	Data      []byte
}

type Thinking struct {
	Text      string
	Signature string
}

func (t Thinking) Empty() bool {
	return t == Thinking{}
}

func BlankText(text string) bool {
	return strings.TrimSpace(text) == ""
}

type Message struct {
	Role            Role
	Content         string
	Images          []Image
	ToolCallID      string
	ToolCalls       []ToolCall
	ToolOutcome     ToolOutcome
	ToolResultBytes int
	ToolExitCode    *int `json:"-"`
	Thinking        Thinking
	Origin          Origin `json:"-"`
}

type Origin struct {
	Source   string
	PostedAt time.Time
	TakenAt  time.Time
}

type Tool struct {
	Name        string
	Description string
	Parameters  any
}

type ToolChoice int

const (
	ToolChoiceAuto ToolChoice = iota
	ToolChoiceNone
)

func (c ToolChoice) OpenAIValue() string {
	switch c {
	case ToolChoiceAuto:
		return ""
	case ToolChoiceNone:
		return "none"
	}
	panic("llm: unknown tool choice")
}

type Request struct {
	Messages   []Message
	Tools      []Tool
	ToolChoice ToolChoice
	OnDelta    func(string)
	OnThinking func(string)
	OnRetry    func()

	OmitThinkingSummary bool
}

func (r Request) Encode(model string) ([]byte, error) {
	if model == "" {
		return nil, transport.Fail("llm.Encode", transport.KindBadRequest, nil, "the model is empty")
	}
	if len(r.Messages) == 0 {
		return nil, transport.Fail("llm.Encode", transport.KindBadRequest, nil, "the request carries no messages")
	}

	messages := make([]wireMessage, 0, len(r.Messages))
	for index, message := range r.Messages {
		if message.Role == RoleAssistant && BlankText(message.Content) && len(message.ToolCalls) == 0 {
			continue
		}
		wire, err := encodeMessage(index, message)
		if err != nil {
			return nil, err
		}
		messages = append(messages, wire)
	}

	tools, err := encodeTools(r.Tools)
	if err != nil {
		return nil, err
	}

	body, err := json.Marshal(wireRequest{
		Model:      model,
		Messages:   messages,
		Tools:      tools,
		ToolChoice: r.ToolChoice.OpenAIValue(),
		Usage:      wireUsageOption{Include: true},
	})
	if err != nil {
		return nil, transport.Fail("llm.Encode", transport.KindBadRequest, err, "encoding the request")
	}
	return body, nil
}

func encodeMessage(index int, message Message) (wireMessage, error) {
	if len(message.Images) > 0 {
		return wireMessage{}, transport.Fail("llm.Encode", transport.KindBadRequest, nil,
			"message %d carries an image; the openrouter wire does not send one", index)
	}
	switch message.Role {
	case RoleSystem, RoleUser:
		if BlankText(message.Content) {
			return wireMessage{}, transport.Fail("llm.Encode", transport.KindBadRequest, nil,
				"message %d is a %s with no content", index, message.Role)
		}
	case RoleTool:
		if message.ToolCallID == "" {
			return wireMessage{}, transport.Fail("llm.Encode", transport.KindBadRequest, nil,
				"message %d is a tool result with no tool call id", index)
		}
	case RoleAssistant:
		if BlankText(message.Content) {
			message.Content = ""
		}
	default:
		return wireMessage{}, transport.Fail("llm.Encode", transport.KindBadRequest, nil,
			"message %d has an unknown role", index)
	}

	calls := make([]wireToolCall, len(message.ToolCalls))
	for callIndex, call := range message.ToolCalls {
		if call.ID == "" || call.Name == "" {
			return wireMessage{}, transport.Fail("llm.Encode", transport.KindBadRequest, nil,
				"message %d tool call %d has no id or no name", index, callIndex)
		}
		calls[callIndex] = wireToolCall{ID: call.ID, Type: "function"}
		calls[callIndex].Function.Name = call.Name
		calls[callIndex].Function.Arguments = string(call.Arguments)
	}

	return wireMessage{
		Role:       message.Role.String(),
		Content:    message.Content,
		ToolCallID: message.ToolCallID,
		ToolCalls:  calls,
	}, nil
}

func encodeTools(tools []Tool) ([]wireTool, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	seen := make(map[string]bool, len(tools))
	encoded := make([]wireTool, len(tools))
	for index, tool := range tools {
		if tool.Name == "" {
			return nil, transport.Fail("llm.Encode", transport.KindBadRequest, nil, "tool %d has no name", index)
		}
		if seen[tool.Name] {
			return nil, transport.Fail("llm.Encode", transport.KindBadRequest, nil, "tool name %q appears twice", tool.Name)
		}
		seen[tool.Name] = true
		encoded[index] = wireTool{Type: "function"}
		encoded[index].Function.Name = tool.Name
		encoded[index].Function.Description = tool.Description
		encoded[index].Function.Parameters = tool.Parameters
	}
	return encoded, nil
}

type wireFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type wireToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function wireFunctionCall `json:"function"`
}

type wireMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	ToolCalls  []wireToolCall `json:"tool_calls,omitempty"`
}

type wireFunction struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

type wireTool struct {
	Type     string       `json:"type"`
	Function wireFunction `json:"function"`
}

type wireUsageOption struct {
	Include bool `json:"include"`
}

type wireRequest struct {
	Model      string          `json:"model"`
	Messages   []wireMessage   `json:"messages"`
	Tools      []wireTool      `json:"tools,omitempty"`
	ToolChoice string          `json:"tool_choice,omitempty"`
	Usage      wireUsageOption `json:"usage"`
}
