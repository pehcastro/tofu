package llm

import (
	"strings"
	"testing"

	"tofu/internal/transport"
)

func TestDecodeRejectsMalformedJSON(t *testing.T) {
	_, err := Decode([]byte(`not json`))
	if transport.KindOf(err) != transport.KindInvalidAnswer {
		t.Fatalf("expected kind invalid_answer, got %v", err)
	}
	if !strings.Contains(err.Error(), "not the expected object") {
		t.Fatalf("error does not name what was wrong: %v", err)
	}
}

func TestDecodeRejectsAProviderError(t *testing.T) {
	_, err := Decode([]byte(`{"error":{"message":"model not found"}}`))
	if transport.KindOf(err) != transport.KindProvider {
		t.Fatalf("expected kind provider, got %v", err)
	}
	if !strings.Contains(err.Error(), "model not found") {
		t.Fatalf("error does not carry the provider message: %v", err)
	}
}

func TestDecodeRejectsAResponseWithNoBuildID(t *testing.T) {
	_, err := Decode([]byte(`{"choices":[{"message":{"content":"hi"}}]}`))
	if transport.KindOf(err) != transport.KindInvalidAnswer {
		t.Fatalf("expected kind invalid_answer, got %v", err)
	}
	if !strings.Contains(err.Error(), "no build id") {
		t.Fatalf("error does not name what was wrong: %v", err)
	}
}

func TestDecodeRejectsAResponseWithNoChoices(t *testing.T) {
	_, err := Decode([]byte(`{"model":"m","choices":[]}`))
	if transport.KindOf(err) != transport.KindInvalidAnswer {
		t.Fatalf("expected kind invalid_answer, got %v", err)
	}
	if !strings.Contains(err.Error(), "no choices") {
		t.Fatalf("error does not name what was wrong: %v", err)
	}
}

func TestDecodeRejectsAnEmptyMessage(t *testing.T) {
	_, err := Decode([]byte(`{"model":"m","choices":[{"finish_reason":"stop","message":{}}]}`))
	if transport.KindOf(err) != transport.KindInvalidAnswer {
		t.Fatalf("expected kind invalid_answer, got %v", err)
	}
	if !strings.Contains(err.Error(), "no content, no tool calls and no refusal") {
		t.Fatalf("error does not name what was wrong: %v", err)
	}
}

func TestDecodeReturnsAMessage(t *testing.T) {
	response, err := Decode([]byte(`{"id":"gen-1","model":"anthropic/claude-fable-5.1","choices":[
{"finish_reason":"stop","message":{"content":"the folder has one file"}}],
"usage":{"prompt_tokens":10,"completion_tokens":5,"cost":0.00002}}`))
	if err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if response.Outcome != OutcomeMessage || response.Content != "the folder has one file" {
		t.Fatalf("response is %+v", response)
	}
	if response.Build != "anthropic/claude-fable-5.1" || response.RequestID != "gen-1" {
		t.Fatalf("response is %+v", response)
	}
	if response.Usage != (Usage{InputTokens: 10, OutputTokens: 5, Cost: 0.00002}) {
		t.Fatalf("usage is %+v", response.Usage)
	}
}

func TestDecodeReturnsARefusalWithText(t *testing.T) {
	response, err := Decode([]byte(`{"id":"gen-2","model":"anthropic/claude-fable-5.1","choices":[
{"finish_reason":"content_filter","message":{"content":"","refusal":"I can't help with that."}}],
"usage":{"prompt_tokens":40,"completion_tokens":0,"cost":0.000001}}`))
	if err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if response.Outcome != OutcomeRefusal || response.Refusal != "I can't help with that." {
		t.Fatalf("response is %+v", response)
	}
}

func TestDecodeReturnsARefusalWithNoText(t *testing.T) {
	response, err := Decode([]byte(`{"id":"gen-3","model":"anthropic/claude-fable-5.1","choices":[
{"finish_reason":"content_filter","message":{"content":""}}],
"usage":{"prompt_tokens":40,"completion_tokens":0,"cost":0.000001}}`))
	if err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if response.Outcome != OutcomeRefusal || response.Refusal != "" {
		t.Fatalf("response is %+v", response)
	}
}

func TestDecodeReturnsToolCallsParsedIntoATypedValue(t *testing.T) {
	response, err := Decode([]byte(`{"id":"gen-4","model":"anthropic/claude-fable-5.1","choices":[
{"finish_reason":"tool_calls","message":{"content":"","tool_calls":[
{"id":"call_1","type":"function","function":{"name":"list_dir","arguments":"{\"path\":\".\"}"}}]}}],
"usage":{"prompt_tokens":50,"completion_tokens":8,"cost":0.00003}}`))
	if err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if response.Outcome != OutcomeToolCalls || len(response.ToolCalls) != 1 {
		t.Fatalf("response is %+v", response)
	}
	call := response.ToolCalls[0]
	if call.ID != "call_1" || call.Name != "list_dir" || string(call.Arguments) != `{"path":"."}` {
		t.Fatalf("tool call is %+v", call)
	}
}

func TestDecodeReportsAnAnswerThatHitTheOutputCapAsTruncated(t *testing.T) {
	response, err := Decode([]byte(`{"id":"gen-6","model":"openai/gpt-5.6","choices":[
{"finish_reason":"length","message":{"content":"half an ans"}}],
"usage":{"prompt_tokens":8016,"completion_tokens":40,"cost":0.0012}}`))
	if err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if response.Outcome != OutcomeTruncated {
		t.Fatalf("an answer that hit the output cap decoded as %s", response.Outcome)
	}
	if response.Content != "half an ans" {
		t.Fatalf("the partial answer is %q", response.Content)
	}
}

func keyPathAnswer(reason, message string) []byte {
	return []byte(`{"id":"gen-7","model":"openai/gpt-5.6","choices":[{"finish_reason":"` + reason +
		`","message":` + message + `}]}`)
}

const (
	keyPathText     = `{"content":"hi"}`
	keyPathToolCall = `{"content":"","tool_calls":[{"id":"call_1","type":"function",` +
		`"function":{"name":"list_dir","arguments":"{}"}}]}`
)

func TestEveryBranchOfTheKeyPathFinishReasonHandlingIsWrittenDown(t *testing.T) {
	cases := []struct {
		reason   string
		withText Outcome
		withCall Outcome
	}{
		{"stop", OutcomeMessage, OutcomeToolCalls},
		{"end_turn", OutcomeMessage, OutcomeToolCalls},
		{"stop_sequence", OutcomeMessage, OutcomeToolCalls},
		{"pause_turn", OutcomeMessage, OutcomeToolCalls},
		{"compaction", OutcomeMessage, OutcomeToolCalls},
		{"completed", OutcomeMessage, OutcomeToolCalls},
		{"length", OutcomeTruncated, OutcomeTruncated},
		{"max_tokens", OutcomeTruncated, OutcomeTruncated},
		{"model_context_window_exceeded", OutcomeTruncated, OutcomeTruncated},
		{"incomplete:max_output_tokens", OutcomeTruncated, OutcomeTruncated},
		{"tool_calls", OutcomeMessage, OutcomeToolCalls},
		{"tool_use", OutcomeMessage, OutcomeToolCalls},
		{"function_call", OutcomeMessage, OutcomeToolCalls},
		{"content_filter", OutcomeRefusal, OutcomeRefusal},
		{"error", OutcomeRefusal, OutcomeRefusal},
		{"refusal", OutcomeRefusal, OutcomeRefusal},
		{"sensitive", OutcomeRefusal, OutcomeRefusal},
		{"invented_next_week", OutcomeMessage, OutcomeToolCalls},
	}
	for _, test := range cases {
		for _, message := range []struct {
			body string
			want Outcome
		}{{keyPathText, test.withText}, {keyPathToolCall, test.withCall}} {
			response, err := Decode(keyPathAnswer(test.reason, message.body))
			if err != nil {
				t.Fatalf("decoding %q with %s: %v", test.reason, message.body, err)
			}
			if response.Outcome != message.want {
				t.Errorf("%q with %s decoded to %s, want %s", test.reason, message.body, response.Outcome, message.want)
			}
		}
	}
}

func TestDecodeNamesAFinishReasonNeitherWireHandles(t *testing.T) {
	response, err := Decode(keyPathAnswer("invented_next_week", keyPathText))
	if err != nil {
		t.Fatalf("an unknown finish reason failed the turn: %v", err)
	}
	if len(response.Warnings) != 1 || !strings.Contains(response.Warnings[0], "invented_next_week") {
		t.Fatalf("warnings are %v", response.Warnings)
	}
	if _, known := MapFinishReason("length"); !known {
		t.Fatal("length is not reported as a handled reason")
	}
}

func TestDecodeRejectsAToolCallWithMalformedArguments(t *testing.T) {
	_, err := Decode([]byte(`{"id":"gen-5","model":"m","choices":[
{"finish_reason":"tool_calls","message":{"content":"","tool_calls":[
{"id":"call_1","type":"function","function":{"name":"list_dir","arguments":"{not json"}}]}}]}`))
	if transport.KindOf(err) != transport.KindInvalidAnswer {
		t.Fatalf("expected kind invalid_answer, got %v", err)
	}
	if !strings.Contains(err.Error(), "list_dir") || !strings.Contains(err.Error(), "not valid json") {
		t.Fatalf("error does not name what was wrong: %v", err)
	}
}
