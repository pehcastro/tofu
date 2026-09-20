package anthropic

import (
	"strings"
	"testing"

	"tofu/internal/transport"
)

func sseText(events ...string) string {
	var out strings.Builder
	for _, event := range events {
		out.WriteString("event: x\ndata: " + event + "\n\n")
	}
	return out.String()
}

func sse(events ...string) *strings.Reader {
	return strings.NewReader(sseText(events...))
}

const (
	eventMessageStart = `{"type":"message_start","message":{"id":"msg_1","model":"claude-opus-4-1-20250805",` +
		`"usage":{"input_tokens":11,"output_tokens":1,"cache_read_input_tokens":3,"cache_creation_input_tokens":4}}}`
	eventTextStart = `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`
	eventTextDelta = `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`
	eventTextStop  = `{"type":"content_block_stop","index":0}`
	eventStopTurn  = `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`
	eventStop      = `{"type":"message_stop"}`
)

func TestReadStreamCompletesATextTurn(t *testing.T) {
	result, err := ReadStream(sse(eventMessageStart, eventTextStart, eventTextDelta, eventTextStop, eventStopTurn, eventStop), true)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if result.Content != "hi" || result.Stop != StopEnd || result.ID != "msg_1" {
		t.Fatalf("result is %+v", result)
	}
	if result.Usage != (Usage{Input: 11, Output: 2, CacheRead: 3, CacheWrite: 4}) {
		t.Fatalf("usage is %+v", result.Usage)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("warnings are %v", result.Warnings)
	}
}

func TestReadStreamFailsWhenNoStartEventArrives(t *testing.T) {
	_, err := ReadStream(sse(eventTextDelta, eventStopTurn, eventStop), true)
	if err == nil {
		t.Fatal("a stream with no message_start was accepted")
	}
	if transport.KindOf(err) != transport.KindInvalidAnswer || !strings.Contains(err.Error(), "before message_start") {
		t.Fatalf("error is %v", err)
	}
}

func TestReadStreamFailsWhenNoTerminalEnvelopeArrives(t *testing.T) {
	_, err := ReadStream(sse(eventMessageStart, eventTextStart, eventTextDelta), true)
	if err == nil {
		t.Fatal("a truncated stream was finalized as a clean stop")
	}
	if transport.KindOf(err) != transport.KindInvalidAnswer || !strings.Contains(err.Error(), "before message_stop") {
		t.Fatalf("error is %v", err)
	}
}

func TestReadStreamAcceptsAStopReasonWithNoTrailingFrame(t *testing.T) {
	result, err := ReadStream(sse(eventMessageStart, eventTextStart, eventTextDelta, eventTextStop, eventStopTurn), true)
	if err != nil {
		t.Fatalf("a stop reason without message_stop was rejected: %v", err)
	}
	if result.Stop != StopEnd || result.Content != "hi" {
		t.Fatalf("result is %+v", result)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "before message_stop") {
		t.Fatalf("warnings are %v", result.Warnings)
	}
}

func TestReadStreamKeepsALeadingUnderscoreOnTheWayBack(t *testing.T) {
	events := sse(
		eventMessageStart,
		`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"__probe"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"path\":"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"go.mod\"}"}}`,
		eventTextStop,
		`{"type":"message_delta","delta":{"stop_reason":"tool_use"}}`,
		eventStop,
	)
	result, err := ReadStream(events, true)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if result.Stop != StopToolUse || len(result.ToolCalls) != 1 {
		t.Fatalf("result is %+v", result)
	}
	if result.ToolCalls[0].Name != "_probe" {
		t.Fatalf("the tool came back as %q, want %q", result.ToolCalls[0].Name, "_probe")
	}
	if string(result.ToolCalls[0].Arguments) != `{"path":"go.mod"}` {
		t.Fatalf("arguments are %s", result.ToolCalls[0].Arguments)
	}
}

func TestUnderscoreToolNameSurvivesTheRoundTrip(t *testing.T) {
	onTheWire := EncodeToolName("_probe", true)
	if onTheWire != "__probe" {
		t.Fatalf("a tool named _probe went out as %q; a short-circuit would lose the underscore", onTheWire)
	}
	if back := DecodeToolName(onTheWire, true); back != "_probe" {
		t.Fatalf("the tool came back as %q", back)
	}
	if EncodeToolName("web_search", true) != "web_search" {
		t.Fatal("a builtin tool must not be prefixed")
	}
	if EncodeToolName("_probe", false) != "_probe" {
		t.Fatal("an api key request must not prefix")
	}
}

func TestMapStopReasonCollapsesTheWireVocabulary(t *testing.T) {
	cases := map[string]Stop{
		"end_turn": StopEnd, "stop_sequence": StopEnd, "pause_turn": StopEnd, "compaction": StopEnd,
		"max_tokens": StopLength, "model_context_window_exceeded": StopLength,
		"tool_use": StopToolUse, "refusal": StopError, "sensitive": StopError,
	}
	for reason, want := range cases {
		got, known := MapStopReason(reason)
		if got != want || !known {
			t.Fatalf("%q mapped to %s (known %v), want %s", reason, got, known, want)
		}
	}
	if got, known := MapStopReason("invented_next_week"); got != StopEnd || known {
		t.Fatalf("an unknown reason mapped to %s (known %v)", got, known)
	}
}

func TestReadStreamDegradesAnUnknownStopReason(t *testing.T) {
	result, err := ReadStream(sse(eventMessageStart,
		`{"type":"message_delta","delta":{"stop_reason":"invented_next_week"}}`, eventStop), true)
	if err != nil {
		t.Fatalf("an unknown stop reason failed the turn: %v", err)
	}
	if result.Stop != StopEnd || result.StopReason != "invented_next_week" {
		t.Fatalf("result is %+v", result)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "unhandled stop reason") {
		t.Fatalf("warnings are %v", result.Warnings)
	}
}

func TestReadStreamFailsOnAnErrorEvent(t *testing.T) {
	_, err := ReadStream(sse(eventMessageStart,
		`{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`), true)
	if transport.KindOf(err) != transport.KindProvider || !strings.Contains(err.Error(), "Overloaded") {
		t.Fatalf("error is %v", err)
	}
}
