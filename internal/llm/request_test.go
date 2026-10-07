package llm

import (
	"encoding/json"
	"testing"
)

func TestAPictureAToolReturnedFollowsItsResultAsAUserMessage(t *testing.T) {
	body, err := Request{Messages: []Message{
		{Role: RoleUser, Content: "look at a.png"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"a.png"}`)}}},
		{Role: RoleTool, ToolCallID: "call-1", Content: "a.png is a png", Images: []Image{{MediaType: "image/png", Data: []byte("made-up png")}}},
	}}.Encode("some/model")
	if err != nil {
		t.Fatalf("a tool result with a picture was refused: %v", err)
	}
	var sent struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatal(err)
	}
	if len(sent.Messages) != 4 || sent.Messages[2].Role != "tool" || string(sent.Messages[2].Content) != `"a.png is a png"` || sent.Messages[3].Role != "user" {
		t.Fatalf("sent %s, want the tool result as text and then a user message holding the picture", body)
	}
	var parts []struct {
		Type     string `json:"type"`
		ImageURL struct {
			URL string `json:"url"`
		} `json:"image_url"`
	}
	if err := json.Unmarshal(sent.Messages[3].Content, &parts); err != nil || len(parts) == 0 || parts[len(parts)-1].Type != "image_url" ||
		parts[len(parts)-1].ImageURL.URL != "data:image/png;base64,bWFkZS11cCBwbmc=" {
		t.Fatalf("the picture was sent as %s, want an image_url part with a data url", sent.Messages[3].Content)
	}
}
