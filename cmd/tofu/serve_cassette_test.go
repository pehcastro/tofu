package main

import (
	"encoding/json"
	"strings"
	"testing"

	"tofu/internal/llm"
)

func TestACassetteRefusesARequestTheAnthropicWireWouldRefuse(t *testing.T) {
	deck, err := readCassette(written(t, t.TempDir(), "one.cassette", `{"text":"answered anyway"}`+"\n"))
	if err != nil {
		t.Fatal(err)
	}
	dangling := llm.Request{Messages: []llm.Message{
		{Role: llm.RoleUser, Content: "read the note"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "toolu_1", Name: "read", Arguments: json.RawMessage(`{"path":"note.txt"}`)}}},
		{Role: llm.RoleUser, Content: "and now?"},
	}}
	if _, err := deck.take(dangling); err == nil || !strings.Contains(err.Error(), "toolu_1") {
		t.Fatalf("a request with an unanswered tool_use was answered from the cassette, err %v", err)
	}
}
