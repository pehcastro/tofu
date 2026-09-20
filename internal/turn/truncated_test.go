package turn

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"boji/internal/llm"
	"boji/internal/llm/wire/anthropic"
)

func truncatedDecision(content string, calls ...llm.ToolCall) llm.Decision {
	return llm.Decision{Build: "m1", Outcome: llm.OutcomeTruncated, Stop: "max_tokens", Content: content, ToolCalls: calls}
}

func TestALengthStopCarryingTwoToolCallsExecutesNeitherAndTellsTheModelWhy(t *testing.T) {
	read := &stubTool{name: "read", result: Result{Content: "file contents"}}
	write := &stubTool{name: "write", result: Result{Content: "wrote it"}}
	model := &stubModel{decisions: []llm.Decision{
		truncatedDecision("I will start by",
			llm.ToolCall{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"a.txt"}`)},
			llm.ToolCall{ID: "call-2", Name: "write", Arguments: json.RawMessage(`{"path":"b.txt","body":"half`)}),
		messageDecision(),
	}}

	row, err := Run(context.Background(), baseConfig(t, model, NewRegistry(read, write)))
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if read.calls != 0 || write.calls != 0 {
		t.Fatalf("a truncated message ran its tool calls: read %d times, write %d times", read.calls, write.calls)
	}

	calls := row.Steps[0].ToolCalls
	if len(calls) != 2 {
		t.Fatalf("expected both dropped calls on the step row, got %+v", calls)
	}
	for _, call := range calls {
		if !strings.Contains(call.Error, theResponseHitTheOutputTokenLimit) {
			t.Fatalf("the row for %s says %q and never names the output token limit", call.Tool, call.Error)
		}
	}

	var results []string
	for _, message := range model.requests[1].Messages {
		if message.Role == llm.RoleTool {
			results = append(results, message.Content)
		}
	}
	told := []string{
		`error: tool call "read" was not executed: ` + theResponseHitTheOutputTokenLimit,
		`error: tool call "write" was not executed: ` + theResponseHitTheOutputTokenLimit,
	}
	if !slices.Equal(results, told) {
		t.Fatalf("the model was sent %q, and each dropped call has to come back naming itself and the reason", results)
	}
}

func TestTheTurnContinuesAfterALengthStopSoTheModelCanReIssueTheCall(t *testing.T) {
	read := &stubTool{name: "read", result: Result{Content: "file contents"}}
	model := &stubModel{decisions: []llm.Decision{
		truncatedDecision("I will read", llm.ToolCall{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"a.tx`)}),
		toolCallDecision(llm.ToolCall{ID: "call-2", Name: "read", Arguments: json.RawMessage(`{"path":"a.txt"}`)}),
		messageDecision(),
	}}

	row, err := Run(context.Background(), baseConfig(t, model, NewRegistry(read)))
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if read.calls != 1 {
		t.Fatalf("the re-issued call ran %d times, and it has to run exactly once", read.calls)
	}
	if row.Outcome != OutcomeStopped {
		t.Fatalf("the turn ended as %s, and a re-issued call that ran ends it normally", row.Outcome)
	}
	if len(row.Steps) != 3 {
		t.Fatalf("expected the truncated step, the re-issued step and the answer, got %d steps", len(row.Steps))
	}
}

func TestALengthStopWithNoToolCallsEndsTheTurnAndTheRowDoesNotSayTheModelFinished(t *testing.T) {
	model := &stubModel{decisions: []llm.Decision{truncatedDecision("the plan is to first")}}

	row, err := Run(context.Background(), baseConfig(t, model, NewRegistry()))
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	read := writeAndReadBack(t, row)
	if read.Outcome == OutcomeStopped {
		t.Fatal("the recorded row says stopped, which is what a model that finished gets")
	}
	if read.Outcome != OutcomeTruncated {
		t.Fatalf("the recorded row says %s, and a cut off answer is truncated", read.Outcome)
	}
	if model.calls != 1 {
		t.Fatalf("the loop asked the model %d times, and there is nothing to retry after a truncated answer with no calls", model.calls)
	}
}

func TestALastAnswerCutOffByTheLimitIsAWarningRatherThanTheAnswer(t *testing.T) {
	read := &stubTool{name: "read", result: Result{Content: "file contents"}}
	model := &stubModel{decisions: []llm.Decision{
		toolCallDecision(llm.ToolCall{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"a.txt"}`)}),
		truncatedDecision("what I did was"),
	}}
	config := baseConfig(t, model, NewRegistry(read))
	config.Caps = Caps{MaxSteps: 1}

	row, err := Run(context.Background(), config)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if row.Outcome != OutcomeStepCap {
		t.Fatalf("the turn ended as %s, and it ran out of steps", row.Outcome)
	}
	if len(row.Steps) != 1 {
		t.Fatalf("a cut off last answer was kept as a step: %+v", row.Steps)
	}
	if len(row.Warnings) != 1 || !strings.Contains(row.Warnings[0], "output token limit") {
		t.Fatalf("the row warns %q, and it has to say the last answer hit the output token limit", row.Warnings)
	}
}

func TestEveryAnthropicStopMapsToAnOutcomeAndAnUnknownOneFails(t *testing.T) {
	for _, stop := range []anthropic.Stop{
		anthropic.StopUnknown, anthropic.StopEnd, anthropic.StopLength, anthropic.StopToolUse, anthropic.StopError,
	} {
		t.Logf("%s with no tool calls is %s, with tool calls %s",
			stop, llm.OutcomeAfter(stop, 0), llm.OutcomeAfter(stop, 1))
	}
	if got := llm.OutcomeAfter(anthropic.StopLength, 2); got != llm.OutcomeTruncated {
		t.Fatalf("a length stop on the anthropic wire is %s, and it has to be truncated", got)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("a stop the wire never defines was mapped instead of failing")
		}
	}()
	llm.OutcomeAfter(anthropic.Stop(99), 0)
}
