package llm

import (
	"encoding/json"
	"strings"
	"testing"

	"tofu/internal/transport"
)

func TestEncodeRefusesAnEmptyModel(t *testing.T) {
	_, err := Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}}.Encode("")
	if transport.KindOf(err) != transport.KindBadRequest {
		t.Fatalf("expected kind bad_request, got %v", err)
	}
}

func TestEncodeRefusesNoMessages(t *testing.T) {
	_, err := Request{}.Encode("m")
	if transport.KindOf(err) != transport.KindBadRequest {
		t.Fatalf("expected kind bad_request, got %v", err)
	}
}

func TestEncodeRefusesAnUnknownRole(t *testing.T) {
	_, err := Request{Messages: []Message{{Role: RoleUnknown, Content: "hi"}}}.Encode("m")
	if transport.KindOf(err) != transport.KindBadRequest {
		t.Fatalf("expected kind bad_request, got %v", err)
	}
}

func TestEncodeRefusesAnImageNamingTheWire(t *testing.T) {
	request := Request{Messages: []Message{{Role: RoleUser, Content: "look",
		Images: []Image{{MediaType: "image/png", Data: []byte("x")}}}}}
	_, err := request.Encode("m")
	if transport.KindOf(err) != transport.KindBadRequest {
		t.Fatalf("expected kind bad_request, got %v", err)
	}
	if !strings.Contains(err.Error(), "openrouter") {
		t.Fatalf("the refusal does not name the wire: %v", err)
	}
}

func TestEncodeRefusesAToolMessageWithNoCallID(t *testing.T) {
	_, err := Request{Messages: []Message{{Role: RoleTool, Content: "result"}}}.Encode("m")
	if transport.KindOf(err) != transport.KindBadRequest {
		t.Fatalf("expected kind bad_request, got %v", err)
	}
}

func TestEncodeRefusesAnAssistantMessageWithNothingToSay(t *testing.T) {
	_, err := Request{Messages: []Message{{Role: RoleAssistant}}}.Encode("m")
	if transport.KindOf(err) != transport.KindBadRequest {
		t.Fatalf("expected kind bad_request, got %v", err)
	}
}

func TestEncodeRefusesADuplicateToolName(t *testing.T) {
	request := Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		Tools:    []Tool{{Name: "read"}, {Name: "read"}},
	}
	_, err := request.Encode("m")
	if transport.KindOf(err) != transport.KindBadRequest {
		t.Fatalf("expected kind bad_request, got %v", err)
	}
}

func TestEncodeRefusesAToolCallWithNoName(t *testing.T) {
	request := Request{
		Messages: []Message{
			{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "call_1"}}},
		},
	}
	_, err := request.Encode("m")
	if transport.KindOf(err) != transport.KindBadRequest {
		t.Fatalf("expected kind bad_request, got %v", err)
	}
}

func TestEncodeProducesTheDocumentedShape(t *testing.T) {
	request := Request{
		Messages: []Message{
			{Role: RoleSystem, Content: "be terse"},
			{Role: RoleUser, Content: "what is in the folder"},
			{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "call_1", Name: "list_dir", Arguments: json.RawMessage(`{"path":"."}`)}}},
			{Role: RoleTool, ToolCallID: "call_1", Content: `["a.txt"]`},
		},
		Tools: []Tool{
			{Name: "list_dir", Description: "lists a directory", Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{"path": map[string]any{"type": "string"}},
				"required":   []string{"path"},
			}},
		},
	}
	body, err := request.Encode("anthropic/claude-fable-5-1")
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("the body is not valid json: %v", err)
	}
	if decoded["model"] != "anthropic/claude-fable-5-1" {
		t.Fatalf("model is %v", decoded["model"])
	}
	messages, ok := decoded["messages"].([]any)
	if !ok || len(messages) != 4 {
		t.Fatalf("expected 4 messages, got %v", decoded["messages"])
	}
	usage, ok := decoded["usage"].(map[string]any)
	if !ok || usage["include"] != true {
		t.Fatalf("expected usage.include true, got %v", decoded["usage"])
	}
	tools, ok := decoded["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %v", decoded["tools"])
	}
	toolCall := messages[2].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)
	function := toolCall["function"].(map[string]any)
	if function["arguments"] != `{"path":"."}` {
		t.Fatalf("tool call arguments are %v", function["arguments"])
	}
}
