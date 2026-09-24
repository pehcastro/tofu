package codex

import (
	"strings"
	"testing"

	"tofu/internal/transport"
)

func sse(events ...string) string {
	var out strings.Builder
	for _, event := range events {
		out.WriteString("data: " + event + "\n\n")
	}
	return out.String()
}

const (
	textItemAdded   = `{"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"msg_1"}}`
	textDelta       = `{"type":"response.output_text.delta","output_index":0,"delta":"ok"}`
	textItemDone    = `{"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"msg_1"}}`
	responseDone    = `{"type":"response.completed","response":{"id":"resp_1","model":"gpt-5.5-codex","status":"completed","usage":{"input_tokens":11,"output_tokens":2,"total_tokens":13,"input_tokens_details":{"cached_tokens":8},"output_tokens_details":{"reasoning_tokens":1}}}}`
	callItemAdded   = `{"type":"response.output_item.added","output_index":1,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"probe"}}`
	callArgsDelta   = `{"type":"response.function_call_arguments.delta","output_index":1,"delta":"{\"question\":\"colour\"}"}`
	callItemDone    = `{"type":"response.output_item.done","output_index":1,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"probe","arguments":"{\"question\":\"colour\"}"}}`
	incompleteDone  = `{"type":"response.incomplete","response":{"id":"resp_2","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"}}}`
	streamFailed    = `{"type":"response.failed","error":{"type":"server_error","code":"oops","message":"the backend gave up"}}`
	metadataWithKey = `{"type":"response.metadata","headers":{"x-codex-turn-state":"turn-state-0000"}}`

	reasoningAdded = `{"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning"}}`
	reasoningDelta = `{"type":"response.reasoning_summary_text.delta","output_index":0,"delta":"thinking"}`
	reasoningDone  = `{"type":"response.output_item.done","output_index":0,"item":{"id":"rs_1","type":"reasoning","encrypted_content":"opaque"}}`

	reasoningDoneNoContent = `{"type":"response.output_item.done","output_index":0,"item":{"id":"rs_2","type":"reasoning"}}`
)

func TestDrainedStreamWithNoTerminalEventIsRetryable(t *testing.T) {
	result, err := ReadStream(strings.NewReader(sse(textItemAdded, textDelta, textItemDone)))
	if err == nil {
		t.Fatalf("a stream that drained with no terminal event was read as a clean stop: %+v", result)
	}
	if transport.KindOf(err).Fatal() {
		t.Fatalf("the failure is fatal rather than retryable: %v", err)
	}
	if result.Stop != StopUnknown {
		t.Fatalf("the truncated turn reports stop %s", result.Stop)
	}
	if result.Content != "ok" {
		t.Fatalf("the partial content was lost: %q", result.Content)
	}
}

func TestTerminalEventIsACleanStop(t *testing.T) {
	result, err := ReadStream(strings.NewReader(sse(textItemAdded, textDelta, textItemDone, responseDone)))
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if result.Stop != StopEnd || result.StopReason != "completed" {
		t.Fatalf("stop is %s %q", result.Stop, result.StopReason)
	}
	if result.ID != "resp_1" || result.Model != "gpt-5.5-codex" || result.Content != "ok" {
		t.Fatalf("result is %+v", result)
	}
	want := Usage{Input: 11, Output: 2, CacheRead: 8, Reasoning: 1, Total: 13}
	if result.Usage != want {
		t.Fatalf("usage is %+v, want %+v", result.Usage, want)
	}
}

func TestToolCallIsAssembledAndReportsToolUse(t *testing.T) {
	result, err := ReadStream(strings.NewReader(
		sse(callItemAdded, callArgsDelta, callItemDone, responseDone)))
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if result.Stop != StopToolUse || len(result.ToolCalls) != 1 {
		t.Fatalf("result is %+v", result)
	}
	call := result.ToolCalls[0]
	if call.ID != "call_1" || call.Name != "probe" || string(call.Arguments) != `{"question":"colour"}` {
		t.Fatalf("the tool call is %+v", call)
	}
}

func TestIncompleteBecomesLength(t *testing.T) {
	result, err := ReadStream(strings.NewReader(sse(textItemAdded, textDelta, incompleteDone)))
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if result.Stop != StopLength || result.StopReason != "incomplete:max_output_tokens" {
		t.Fatalf("stop is %s %q", result.Stop, result.StopReason)
	}
}

func TestStreamErrorEventFails(t *testing.T) {
	_, err := ReadStream(strings.NewReader(sse(streamFailed)))
	if err == nil || !strings.Contains(err.Error(), "the backend gave up") {
		t.Fatalf("a failed stream gave %v", err)
	}
}

func TestMetadataCarriesTheTurnState(t *testing.T) {
	result, err := ReadStream(strings.NewReader(sse(metadataWithKey, textItemAdded, textDelta, responseDone)))
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if result.TurnState != "turn-state-0000" {
		t.Fatalf("the turn state is %q", result.TurnState)
	}
}

func TestWhitespaceArgumentLoopIsCut(t *testing.T) {
	events := []string{callItemAdded}
	for range WhitespaceDeltaCap {
		events = append(events, `{"type":"response.function_call_arguments.delta","output_index":1,"delta":"  "}`)
	}
	_, err := ReadStream(strings.NewReader(sse(events...)))
	if err == nil || !strings.Contains(err.Error(), "without progressing") {
		t.Fatalf("a whitespace-only argument loop gave %v", err)
	}
	if transport.KindOf(err).Fatal() {
		t.Fatalf("the whitespace loop failure is fatal rather than retryable: %v", err)
	}
}

func TestAReasoningItemIsCapturedWithItsEncryptedContent(t *testing.T) {
	result, err := ReadStream(strings.NewReader(
		sse(reasoningAdded, reasoningDelta, reasoningDone, callItemAdded, callArgsDelta, callItemDone, responseDone)))
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if result.ReasoningID != "rs_1" || result.ReasoningEncrypted != "opaque" {
		t.Fatalf("the reasoning item is %+v", result)
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("the tool call was lost alongside the reasoning item: %+v", result.ToolCalls)
	}
}

func TestAReasoningItemWithNoEncryptedContentStillCompletesTheTurn(t *testing.T) {
	result, err := ReadStream(strings.NewReader(
		sse(reasoningAdded, reasoningDelta, reasoningDoneNoContent, textItemAdded, textDelta, textItemDone, responseDone)))
	if err != nil {
		t.Fatalf("a reasoning item with no encrypted content failed the turn: %v", err)
	}
	if result.ReasoningID != "rs_2" || result.ReasoningEncrypted != "" {
		t.Fatalf("the reasoning item is %+v", result)
	}
	if result.Content != "ok" {
		t.Fatalf("the rest of the turn did not complete: %+v", result)
	}
}

func TestUnknownEventsAreIgnored(t *testing.T) {
	result, err := ReadStream(strings.NewReader(sse(
		`{"type":"response.created","response":{"id":"resp_1"}}`,
		`{"type":"response.in_progress"}`,
		textItemAdded, textDelta, textItemDone, responseDone)))
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if result.Content != "ok" || len(result.Warnings) != 0 {
		t.Fatalf("result is %+v", result)
	}
}
