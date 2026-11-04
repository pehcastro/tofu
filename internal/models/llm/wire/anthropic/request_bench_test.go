package anthropic

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"tofu/internal/models/llm/core"
)

func BenchmarkEncodeALongConversation(b *testing.B) {
	messages := []core.Message{{Role: core.RoleUser, Content: "read every file"}}
	result := strings.Repeat("func f() { return next(x) }\n", 400)
	for i := range 120 {
		id := "call-" + strconv.Itoa(i)
		messages = append(messages,
			core.Message{Role: core.RoleAssistant, Content: "reading", ToolCalls: []core.ToolCall{{ID: id, Name: "read", Arguments: json.RawMessage(`{"path":"a.go"}`)}}},
			core.Message{Role: core.RoleTool, ToolCallID: id, Content: result})
	}
	r := Request{Model: "claude-opus-5", System: []string{"you are a coder"}, Messages: messages, ToolChoice: core.ToolChoiceAuto}
	body, err := r.Encode(true)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		_, _ = r.Encode(true)
	}
	b.ReportMetric(float64(len(body)), "body-bytes")
}
