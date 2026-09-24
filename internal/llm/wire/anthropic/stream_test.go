package anthropic

import (
	"strings"
	"testing"

	"tofu/internal/llm"
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
	result, err := ReadStream(sse(eventMessageStart, eventTextStart, eventTextDelta, eventTextStop, eventStopTurn, eventStop), true, nil)
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
	_, err := ReadStream(sse(eventTextDelta, eventStopTurn, eventStop), true, nil)
	if err == nil {
		t.Fatal("a stream with no message_start was accepted")
	}
	if transport.KindOf(err) != transport.KindInvalidAnswer || !strings.Contains(err.Error(), "before message_start") {
		t.Fatalf("error is %v", err)
	}
}

func TestReadStreamFailsWhenNoTerminalEnvelopeArrives(t *testing.T) {
	_, err := ReadStream(sse(eventMessageStart, eventTextStart, eventTextDelta), true, nil)
	if err == nil {
		t.Fatal("a truncated stream was finalized as a clean stop")
	}
	if transport.KindOf(err) != transport.KindInvalidAnswer || !strings.Contains(err.Error(), "before message_stop") {
		t.Fatalf("error is %v", err)
	}
}

func TestReadStreamAcceptsAStopReasonWithNoTrailingFrame(t *testing.T) {
	result, err := ReadStream(sse(eventMessageStart, eventTextStart, eventTextDelta, eventTextStop, eventStopTurn), true, nil)
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
	result, err := ReadStream(events, true, nil)
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

func TestMapFinishReasonCollapsesTheWireVocabulary(t *testing.T) {
	cases := map[string]Stop{
		"end_turn": StopEnd, "stop_sequence": StopEnd, "pause_turn": StopEnd, "compaction": StopEnd,
		"max_tokens": StopLength, "model_context_window_exceeded": StopLength,
		"tool_use": StopToolUse, "refusal": StopError, "sensitive": StopError,
	}
	for reason, want := range cases {
		got, handled := llm.MapFinishReason(reason)
		if got != want || !handled {
			t.Fatalf("%q mapped to %s (handled %v), want %s", reason, got, handled, want)
		}
	}
	if got, handled := llm.MapFinishReason("invented_next_week"); got != StopEnd || handled {
		t.Fatalf("an unknown reason mapped to %s (handled %v)", got, handled)
	}
}

func TestTheKeyPathAndTheSubscriptionAgreeOnEveryStopReasonEitherWireSends(t *testing.T) {
	reasons := []string{
		"stop", "end_turn", "stop_sequence", "pause_turn", "compaction", "completed",
		"length", "max_tokens", "model_context_window_exceeded", "incomplete:max_output_tokens",
		"tool_calls", "tool_use", "function_call",
		"content_filter", "error", "refusal", "sensitive",
		"invented_next_week",
	}
	for _, reason := range reasons {
		streamed, err := ReadStream(sse(eventMessageStart, eventTextStart, eventTextDelta, eventTextStop,
			`{"type":"message_delta","delta":{"stop_reason":"`+reason+`"}}`, eventStop), true, nil)
		if err != nil {
			t.Fatalf("streaming %q: %v", reason, err)
		}
		subscription := llm.OutcomeAfter(streamed.Stop, len(streamed.ToolCalls))

		decoded, err := llm.Decode([]byte(`{"id":"gen-1","model":"m","choices":[{"finish_reason":"` + reason +
			`","message":{"content":"hi"}}]}`))
		if err != nil {
			t.Fatalf("decoding %q: %v", reason, err)
		}
		if decoded.Outcome != subscription {
			t.Errorf("%q is %s on the key path and %s on the subscription path", reason, decoded.Outcome, subscription)
		}
	}
}

func TestReadStreamDegradesAnUnknownStopReason(t *testing.T) {
	result, err := ReadStream(sse(eventMessageStart,
		`{"type":"message_delta","delta":{"stop_reason":"invented_next_week"}}`, eventStop), true, nil)
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

func multiDeltaTextEvents() []string {
	return []string{
		eventMessageStart,
		eventTextStart,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"The gate reads "}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"toolgate.go "}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"before the policy."}}`,
		eventTextStop,
		eventStopTurn,
		eventStop,
	}
}

func TestReadStreamReportsEveryTextDeltaAsItArrives(t *testing.T) {
	var deltas []string
	result, err := ReadStream(sse(multiDeltaTextEvents()...), true, func(text string) { deltas = append(deltas, text) })
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if len(deltas) != 3 {
		t.Fatalf("%d deltas arrived, want 3: %q", len(deltas), deltas)
	}
	if joined := strings.Join(deltas, ""); joined != result.Content {
		t.Fatalf("the deltas joined read %q, want the result content %q", joined, result.Content)
	}
}

func TestReadStreamDeltasMatchTheNonStreamingContentByteForByte(t *testing.T) {
	streamed, err := ReadStream(sse(multiDeltaTextEvents()...), true, nil)
	if err != nil {
		t.Fatalf("reading without a delta callback: %v", err)
	}
	var deltas []string
	withDeltas, err := ReadStream(sse(multiDeltaTextEvents()...), true, func(text string) { deltas = append(deltas, text) })
	if err != nil {
		t.Fatalf("reading with a delta callback: %v", err)
	}
	if strings.Join(deltas, "") != streamed.Content {
		t.Fatalf("the streamed deltas read %q, want the non-streaming content %q byte for byte",
			strings.Join(deltas, ""), streamed.Content)
	}
	if withDeltas.Content != streamed.Content {
		t.Fatalf("a delta callback changed the decoded content: %q vs %q", withDeltas.Content, streamed.Content)
	}
}

func TestReadStreamAccumulatesTheThinkingSignatureAcrossDeltas(t *testing.T) {
	events := sse(
		eventMessageStart,
		`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"weighing the options"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig_part_one_"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig_part_two"}}`,
		eventTextStop,
		`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"read"}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{}"}}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"message_delta","delta":{"stop_reason":"tool_use"}}`,
		eventStop,
	)
	result, err := ReadStream(events, true, nil)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if result.Thinking != "weighing the options" {
		t.Fatalf("thinking is %q", result.Thinking)
	}
	if result.ThinkingSignature != "sig_part_one_sig_part_two" {
		t.Fatalf("signature is %q, want the two deltas joined", result.ThinkingSignature)
	}
}

func TestReadStreamFailsOnAnErrorEvent(t *testing.T) {
	_, err := ReadStream(sse(eventMessageStart,
		`{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`), true, nil)
	if transport.KindOf(err) != transport.KindProvider || !strings.Contains(err.Error(), "Overloaded") {
		t.Fatalf("error is %v", err)
	}
}
