package codex

import (
	"encoding/json"
	"slices"
	"testing"

	"tofu/internal/llm"
)

func TestAPictureAToolReturnedTravelsInItsFunctionCallOutput(t *testing.T) {
	items, err := encodeInput([]llm.Message{
		{Role: llm.RoleUser, Content: "look at a.png"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"a.png"}`)}}},
		{Role: llm.RoleTool, ToolCallID: "call-1", Content: "a.png is a png", Images: []llm.Image{{MediaType: "image/png", Data: []byte("made-up png")}}},
	})
	if err != nil {
		t.Fatalf("a tool result with a picture was refused: %v", err)
	}
	body, err := json.Marshal(items[len(items)-1])
	if err != nil {
		t.Fatal(err)
	}
	var output struct {
		Type   string `json:"type"`
		Output []struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			ImageURL string `json:"image_url"`
		} `json:"output"`
	}
	if err := json.Unmarshal(body, &output); err != nil {
		t.Fatalf("the result was sent as %s, want an output array: %v", body, err)
	}
	if output.Type != "function_call_output" || len(output.Output) != 2 || output.Output[0].Text != "a.png is a png" ||
		output.Output[1].Type != "input_image" || output.Output[1].ImageURL != "data:image/png;base64,bWFkZS11cCBwbmc=" {
		t.Fatalf("the result was sent as %s, want the text then the picture as an input_image", body)
	}
}

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
