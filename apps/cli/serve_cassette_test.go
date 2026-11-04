package main

import (
	"encoding/json"
	"strings"
	"testing"

	"tofu/internal/models/llm/core"
)

func TestACassetteRefusesARequestTheAnthropicWireWouldRefuse(t *testing.T) {
	deck, err := readCassette(written(t, t.TempDir(), "one.cassette", `{"text":"answered anyway"}`+"\n"))
	if err != nil {
		t.Fatal(err)
	}
	dangling := core.Request{Messages: []core.Message{
		{Role: core.RoleUser, Content: "read the note"},
		{Role: core.RoleAssistant, ToolCalls: []core.ToolCall{{ID: "toolu_1", Name: "read", Arguments: json.RawMessage(`{"path":"note.txt"}`)}}},
		{Role: core.RoleUser, Content: "and now?"},
	}}
	if _, err := deck.take(dangling); err == nil || !strings.Contains(err.Error(), "toolu_1") {
		t.Fatalf("a request with an unanswered tool_use was answered from the cassette, err %v", err)
	}
}
