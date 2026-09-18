package llm

import (
	"strings"
	"testing"

	"boji/internal/transport"
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
