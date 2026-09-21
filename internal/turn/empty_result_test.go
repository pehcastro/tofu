package turn

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"tofu/internal/llm"
)

func ranQuietly(t *testing.T, exitCode int) []llm.Message {
	t.Helper()
	quiet := &stubTool{name: "bash", result: Result{Command: "true", ExitCode: &exitCode}}
	model := &stubModel{decisions: []llm.Decision{
		toolCallDecision(llm.ToolCall{ID: "call-1", Name: "bash", Arguments: json.RawMessage(`{"command":"true"}`)}),
		messageDecision(),
	}}
	if _, err := Run(context.Background(), baseConfig(t, model, NewRegistry(quiet))); err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	return model.requests[1].Messages
}

func toolResultIn(t *testing.T, messages []llm.Message) string {
	t.Helper()
	for _, message := range messages {
		if message.Role == llm.RoleTool {
			return message.Content
		}
	}
	t.Fatal("the model was sent no tool result at all")
	return ""
}

func TestACommandThatPrintsNothingStillSendsAContentFieldOnBothWires(t *testing.T) {
	messages := ranQuietly(t, 0)

	openrouter, err := (llm.Request{Messages: messages}).Encode("m1")
	if err != nil {
		t.Fatalf("the openrouter request does not encode: %v", err)
	}
	var decoded struct {
		Messages []map[string]json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(openrouter, &decoded); err != nil {
		t.Fatalf("reading back the openrouter request: %v", err)
	}
	for _, message := range decoded.Messages {
		if string(message["role"]) != `"tool"` {
			continue
		}
		if _, carried := message["content"]; !carried {
			t.Fatalf("the openrouter tool message %v has no content field, and the schema requires one", message)
		}
	}

	for _, message := range encodedAnthropicMessages(t, messages) {
		for _, block := range message.Content {
			if block.Type == "tool_result" && block.Content == "" {
				t.Fatalf("the anthropic tool_result for %s went out with no content", block.ToolUseID)
			}
		}
	}

	if result := toolResultIn(t, messages); !strings.Contains(result, "printed nothing") {
		t.Fatalf("the tool result says %q, and a command that printed nothing has to say so", result)
	}
}

func TestAnEmptySuccessAndAnEmptyFailureReadDifferently(t *testing.T) {
	succeeded := toolResultIn(t, ranQuietly(t, 0))
	failed := toolResultIn(t, ranQuietly(t, 1))
	if succeeded == failed {
		t.Fatalf("a command that succeeded and one that failed both read %q", succeeded)
	}
	if !strings.Contains(succeeded, "succeeded") || !strings.Contains(failed, "failed") {
		t.Fatalf("success reads %q and failure reads %q, and the model has to tell them apart", succeeded, failed)
	}
}
