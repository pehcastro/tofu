package anthropic

import (
	"encoding/json"
	"testing"

	"tofu/internal/llm"
)

func TestASignatureWithNoTextSendsNoThinkingBlock(t *testing.T) {
	messages := encodedThinkingMessages(t, llm.Thinking{Signature: "sig-only"})
	for _, block := range messages[1].Content {
		if block.Type == "thinking" {
			t.Fatalf("a signature with no text produced a thinking block: %+v", block)
		}
	}
}

func TestTextWithNoSignatureSendsAThinkingBlockWithNoSignature(t *testing.T) {
	block := encodedThinkingMessages(t, llm.Thinking{Text: "reasoning about it"})[1].Content[0]
	if block.Type != "thinking" || block.Thinking != "reasoning about it" || block.Signature != "" {
		t.Fatalf("text with no signature encoded as %+v", block)
	}
}

func encodedThinkingMessages(t *testing.T, thinking llm.Thinking) []wireMessage {
	t.Helper()
	request := minimalRequest()
	request.Messages = append(request.Messages,
		llm.Message{Role: llm.RoleAssistant, Thinking: thinking,
			ToolCalls: []llm.ToolCall{{ID: "toolu_1", Name: "read"}}},
		llm.Message{Role: llm.RoleTool, ToolCallID: "toolu_1", Content: "a file"})

	body, err := request.Encode(true)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	var decoded struct {
		Messages []wireMessage `json:"messages"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	return decoded.Messages
}
