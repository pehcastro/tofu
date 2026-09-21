package turn

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/llm/wire/anthropic"
)

type encodedBlock struct {
	Type      string `json:"type"`
	Text      string `json:"text"`
	ID        string `json:"id"`
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
}

type encodedMessage struct {
	Role    string         `json:"role"`
	Content []encodedBlock `json:"content"`
}

func encodedAnthropicMessages(t *testing.T, messages []llm.Message) []encodedMessage {
	t.Helper()
	body, err := anthropic.Request{Model: "m1", Messages: messages, MaxTokens: 1024}.Encode(false)
	if err != nil {
		t.Fatalf("the request the model was sent does not encode: %v", err)
	}
	var encoded struct {
		Messages []encodedMessage `json:"messages"`
	}
	if err := json.Unmarshal(body, &encoded); err != nil {
		t.Fatalf("reading back the encoded request: %v", err)
	}
	return encoded.Messages
}

func TestAGuardTripLeavesNoToolCallWithoutAResultAndStillGetsALastWord(t *testing.T) {
	repeated := func(id string) llm.ToolCall {
		return llm.ToolCall{ID: id, Name: "noop", Arguments: json.RawMessage(`{}`)}
	}
	var decisions []llm.Decision
	for i := 1; i < konst.TurnLoopGuardRepeats; i++ {
		decisions = append(decisions, toolCallDecision(repeated("call-"+strconv.Itoa(i))))
	}
	decisions = append(decisions,
		toolCallDecision(repeated("trip-1"), repeated("trip-2"), repeated("trip-3")),
		messageDecision())
	model := &stubModel{decisions: decisions}

	row, err := Run(context.Background(), fixedResultConfig(t, model, Caps{MaxSteps: 10}))
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if row.Outcome != OutcomeLoopGuard {
		t.Fatalf("outcome = %s, want the guard to stop the turn", row.Outcome)
	}
	if model.calls != len(model.decisions) {
		t.Fatalf("the model was asked %d times, and the last word is the %dth", model.calls, len(model.decisions))
	}

	messages := encodedAnthropicMessages(t, model.requests[len(model.requests)-1].Messages)
	answered := map[string]string{}
	var asked []string
	for _, message := range messages {
		for _, block := range message.Content {
			switch block.Type {
			case "tool_use":
				asked = append(asked, block.ID)
			case "tool_result":
				answered[block.ToolUseID] = block.Content
			}
		}
	}
	for _, id := range asked {
		if _, ok := answered[id]; !ok {
			t.Fatalf("tool call %s went out with no result, and a vendor refuses that request", id)
		}
	}
	for _, id := range []string{"trip-2", "trip-3"} {
		if !strings.Contains(answered[id], "same arguments and returned the same result") {
			t.Fatalf("the unanswered call %s carries %q instead of the guard's own reason", id, answered[id])
		}
	}

	last := messages[len(messages)-1]
	if last.Role != "user" || !strings.Contains(last.Content[0].Text, andThisIsItsLastStep) {
		t.Fatalf("the last message sent is %+v, and it has to be the request for a last word", last)
	}
}
