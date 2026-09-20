package turn

import (
	"encoding/json"
	"testing"

	"tofu/internal/llm"
)

func TestHistoryNumbersEntriesByStepAndKeepsThemAlignedWithTheMessages(t *testing.T) {
	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: "the rules"},
		{Role: llm.RoleUser, Content: "the task"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "a", Name: "read", Arguments: json.RawMessage(`{"path":"one.ts"}`)}}},
		{Role: llm.RoleTool, ToolCallID: "a", Content: "one"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "b", Name: "read", Arguments: json.RawMessage(`{"path":"one.ts"}`)}}},
		{Role: llm.RoleTool, ToolCallID: "b", Content: "one again"},
	}

	conversation := historyOf(messages)

	if conversation.Instructions != "the rules" {
		t.Fatalf("instructions = %q, want the system message alone", conversation.Instructions)
	}
	if offset := len(messages) - len(conversation.Entries); offset != 1 {
		t.Fatalf("offset = %d, want 1: compaction writes an entry back to messages[offset+i] and that only holds if every non-system message made exactly one entry", offset)
	}
	steps := []int{0, 1, 1, 2, 2}
	for i, want := range steps {
		if conversation.Entries[i].Step != want {
			t.Fatalf("entry %d is on step %d, want %d", i, conversation.Entries[i].Step, want)
		}
	}
	first, second := conversation.Entries[2], conversation.Entries[4]
	if first.Tool != "read" || second.Tool != "read" {
		t.Fatalf("the tool results did not pick up their tool name: %q and %q", first.Tool, second.Tool)
	}
	if first.SupersedeKey != second.SupersedeKey {
		t.Fatalf("two reads of the same path got different supersede keys, %q and %q, so the second never supersedes the first",
			first.SupersedeKey, second.SupersedeKey)
	}
	if conversation.Entries[1].Tool != "" {
		t.Fatalf("the assistant message was recorded as a %q result and compaction may now drop it", conversation.Entries[1].Tool)
	}
}
