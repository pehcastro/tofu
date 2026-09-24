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

func MarkStablePrefix(body []byte) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return nil, transport.Fail("openrouter.MarkStablePrefix", transport.KindBadRequest, err, "the request body did not parse")
	}

	var messages []map[string]json.RawMessage
	if raw, present := fields["messages"]; present {
		if err := json.Unmarshal(raw, &messages); err != nil {
			return nil, transport.Fail("openrouter.MarkStablePrefix", transport.KindBadRequest, err, "the messages did not parse")
		}
	}
	var tools []map[string]json.RawMessage
	if raw, present := fields["tools"]; present {
		if err := json.Unmarshal(raw, &tools); err != nil {
			return nil, transport.Fail("openrouter.MarkStablePrefix", transport.KindBadRequest, err, "the tools did not parse")
		}
	}

	lastSystem := -1
	for index, message := range messages {
		var role string
		if err := json.Unmarshal(message["role"], &role); err != nil {
			return nil, transport.Fail("openrouter.MarkStablePrefix", transport.KindBadRequest, err, "message %d has no readable role", index)
		}
		if role == "system" {
			lastSystem = index
		}
	}
	if lastSystem < 0 && len(tools) == 0 {
		return body, nil
	}

	if lastSystem >= 0 {
		var text string
		if err := json.Unmarshal(messages[lastSystem]["content"], &text); err != nil {
			return nil, transport.Fail("openrouter.MarkStablePrefix", transport.KindBadRequest, err,
				"the system message content was not a plain string")
		}
		marked, err := json.Marshal([]contentPart{{Type: "text", Text: text, CacheControl: &cacheControl{Type: "ephemeral"}}})
		if err != nil {
			return nil, transport.Fail("openrouter.MarkStablePrefix", transport.KindBadRequest, err, "encoding the marked system block")
		}
		messages[lastSystem]["content"] = marked
		if fields["messages"], err = json.Marshal(messages); err != nil {
			return nil, transport.Fail("openrouter.MarkStablePrefix", transport.KindBadRequest, err, "encoding the marked messages")
		}
	}
	if len(tools) > 0 {
		tools[len(tools)-1]["cache_control"] = json.RawMessage(`{"type":"ephemeral"}`)
		marked, err := json.Marshal(tools)
		if err != nil {
			return nil, transport.Fail("openrouter.MarkStablePrefix", transport.KindBadRequest, err, "encoding the marked tools")
		}
		fields["tools"] = marked
	}

	out, err := json.Marshal(fields)
	if err != nil {
		return nil, transport.Fail("openrouter.MarkStablePrefix", transport.KindBadRequest, err, "encoding the marked request")
	}
	return out, nil
}
