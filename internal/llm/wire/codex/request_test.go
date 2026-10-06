package codex

import (
	"encoding/json"
	"slices"
	"testing"

	"tofu/internal/llm"
)

func TestAReplyWithReasoningTextAndACallIsSentBackInTheOrderItArrived(t *testing.T) {
	call := llm.ToolCall{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"note.txt"}`)}
	reasoning := llm.Thinking{Signature: EncodeReasoning("rs_1", "sealed")}
	for _, c := range []struct {
		name  string
		reply llm.Message
		want  []string
	}{
		{name: "reasoning, text and a call", reply: llm.Message{Role: llm.RoleAssistant, Content: "reading the note first", ToolCalls: []llm.ToolCall{call}, Thinking: reasoning},
			want: []string{"reasoning", "message", "function_call"}},
		{name: "text of only whitespace", reply: llm.Message{Role: llm.RoleAssistant, Content: " \n\t", ToolCalls: []llm.ToolCall{call}, Thinking: reasoning},
			want: []string{"reasoning", "function_call"}},
		{name: "text and a call with no reasoning", reply: llm.Message{Role: llm.RoleAssistant, Content: "reading the note first", ToolCalls: []llm.ToolCall{call}},
			want: []string{"message", "function_call"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			items, err := encodeInput([]llm.Message{{Role: llm.RoleUser, Content: "read the note"}, c.reply})
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, item := range items[1:] {
				got = append(got, item.Type)
			}
			if !slices.Equal(got, c.want) {
				t.Errorf("the reply was sent as %v, want %v", got, c.want)
			}
		})
	}
}
