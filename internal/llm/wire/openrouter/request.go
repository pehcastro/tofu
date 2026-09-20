package openrouter

import (
	"encoding/json"

	"tofu/internal/transport"
)

type cacheControl struct {
	Type string `json:"type"`
}

type contentPart struct {
	Type         string        `json:"type"`
	Text         string        `json:"text"`
	CacheControl *cacheControl `json:"cache_control,omitempty"`
}

type wireFunction struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

type wireTool struct {
	Type         string        `json:"type"`
	Function     wireFunction  `json:"function"`
	CacheControl *cacheControl `json:"cache_control,omitempty"`
}

type wireToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type wireMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	ToolCalls  []wireToolCall  `json:"tool_calls,omitempty"`
}

type wireUsageOption struct {
	Include bool `json:"include"`
}

type wireBody struct {
	Model    string          `json:"model"`
	Messages []wireMessage   `json:"messages"`
	Tools    []wireTool      `json:"tools,omitempty"`
	Usage    wireUsageOption `json:"usage"`
}

func MarkStablePrefix(body []byte) ([]byte, error) {
	var wire wireBody
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, transport.Fail("openrouter.MarkStablePrefix", transport.KindBadRequest, err, "the request body did not parse")
	}

	lastSystem := -1
	for index, message := range wire.Messages {
		if message.Role == "system" {
			lastSystem = index
		}
	}
	if lastSystem < 0 && len(wire.Tools) == 0 {
		return body, nil
	}

	if lastSystem >= 0 {
		var text string
		if err := json.Unmarshal(wire.Messages[lastSystem].Content, &text); err != nil {
			return nil, transport.Fail("openrouter.MarkStablePrefix", transport.KindBadRequest, err,
				"the system message content was not a plain string")
		}
		marked, err := json.Marshal([]contentPart{{Type: "text", Text: text, CacheControl: &cacheControl{Type: "ephemeral"}}})
		if err != nil {
			return nil, transport.Fail("openrouter.MarkStablePrefix", transport.KindBadRequest, err, "encoding the marked system block")
		}
		wire.Messages[lastSystem].Content = marked
	}
	if len(wire.Tools) > 0 {
		wire.Tools[len(wire.Tools)-1].CacheControl = &cacheControl{Type: "ephemeral"}
	}

	out, err := json.Marshal(wire)
	if err != nil {
		return nil, transport.Fail("openrouter.MarkStablePrefix", transport.KindBadRequest, err, "encoding the marked request")
	}
	return out, nil
}
