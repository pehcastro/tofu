package codex

import (
	"encoding/json"
	"strings"
	"testing"

	"tofu/internal/llm"
)

func decodeBody(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("the encoded body is not json: %v", err)
	}
	return decoded
}

func hello() []llm.Message {
	return []llm.Message{{Role: llm.RoleUser, Content: "hello"}}
}

func TestSamplingControlsNeverReachTheBody(t *testing.T) {
	temperature, topP, minP := 0.7, 0.9, 0.1
	topK := 40
	request := Request{
		Model:           "gpt-5.5-codex",
		Messages:        hello(),
		MaxOutputTokens: 4096,
		Sampling: Sampling{
			Temperature:       &temperature,
			TopP:              &topP,
			TopK:              &topK,
			MinP:              &minP,
			PresencePenalty:   &temperature,
			FrequencyPenalty:  &temperature,
			RepetitionPenalty: &temperature,
			Stop:              []string{"\n\n"},
		},
	}
	body, err := request.Encode(nil)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	decoded := decodeBody(t, body)
	for _, control := range append(ForbiddenSamplingControls(), "max_output_tokens", "max_completion_tokens") {
		if _, present := decoded[control]; present {
			t.Fatalf("%q reached the body:\n%s", control, body)
		}
	}
	refused := request.RefusedControls()
	if len(refused) != 9 {
		t.Fatalf("the caller was told about %v, want every control it set", refused)
	}
}

func TestStoreAndStreamAreForced(t *testing.T) {
	body, err := Request{Model: "gpt-5.5-codex", Messages: hello()}.Encode(nil)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	decoded := decodeBody(t, body)
	if decoded["store"] != false || decoded["stream"] != true {
		t.Fatalf("store and stream are %v and %v", decoded["store"], decoded["stream"])
	}
	include, _ := decoded["include"].([]any)
	if len(include) != 1 || include[0] != EncryptedReasoningInclude {
		t.Fatalf("include is %v", decoded["include"])
	}
}

func TestReasoningEffortIsAClosedVocabulary(t *testing.T) {
	if _, err := (Request{Model: "m", Messages: hello(), Effort: "turbo"}).Encode(nil); err == nil {
		t.Fatal("an effort outside the vocabulary was accepted")
	}
	body, err := Request{Model: "m", Messages: hello(), Effort: "high"}.Encode(nil)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	reasoning, _ := decodeBody(t, body)["reasoning"].(map[string]any)
	if reasoning["effort"] != "high" || reasoning["summary"] != DefaultReasoningSummary {
		t.Fatalf("reasoning is %v", reasoning)
	}

	body, err = Request{Model: "m", Messages: hello(), Effort: "high", SummaryOff: true}.Encode(nil)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	reasoning, _ = decodeBody(t, body)["reasoning"].(map[string]any)
	if _, present := reasoning["summary"]; present {
		t.Fatalf("an explicit opt out still asked for a summary: %v", reasoning)
	}
	if _, present := decodeBody(t, mustEncode(t, Request{Model: "m", Messages: hello()}))["reasoning"]; present {
		t.Fatal("a request with no effort carried a reasoning field")
	}
}

func mustEncode(t *testing.T, request Request) []byte {
	t.Helper()
	body, err := request.Encode(nil)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	return body
}

func TestInputCarriesTheToolRoundTrip(t *testing.T) {
	body := mustEncode(t, Request{
		Model:        "gpt-5.5-codex",
		Instructions: "be brief",
		Tools: []llm.Tool{{Name: "probe", Description: "a probe",
			Parameters: map[string]any{"type": "object"}}},
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: "call probe"},
			{Role: llm.RoleAssistant, Content: "calling", ToolCalls: []llm.ToolCall{
				{ID: "call_1", Name: "probe", Arguments: json.RawMessage(`{"question":"colour"}`)}}},
			{Role: llm.RoleTool, ToolCallID: "call_1", Content: "chartreuse"},
		},
	})
	decoded := decodeBody(t, body)
	if decoded["instructions"] != "be brief" {
		t.Fatalf("instructions are %v", decoded["instructions"])
	}
	input, _ := decoded["input"].([]any)
	if len(input) != 4 {
		t.Fatalf("input carries %d items:\n%s", len(input), body)
	}
	kinds := make([]string, len(input))
	for index, item := range input {
		object, _ := item.(map[string]any)
		kind, _ := object["type"].(string)
		if kind == "" {
			kind, _ = object["role"].(string)
		}
		kinds[index] = kind
	}
	want := []string{"user", "message", "function_call", "function_call_output"}
	for index, kind := range want {
		if kinds[index] != kind {
			t.Fatalf("input item %d is %q, want %q:\n%s", index, kinds[index], kind, body)
		}
	}
	tools, _ := decoded["tools"].([]any)
	first, _ := tools[0].(map[string]any)
	if first["type"] != "function" || first["name"] != "probe" {
		t.Fatalf("the tool encoded as %v", first)
	}
}

func TestSystemMessagesAreRefused(t *testing.T) {
	_, err := Request{Model: "m", Messages: []llm.Message{{Role: llm.RoleSystem, Content: "x"}}}.Encode(nil)
	if err == nil || !strings.Contains(err.Error(), "instructions") {
		t.Fatalf("a system message gave %v", err)
	}
}

func TestTurnMetadataIsAFixedSizeAsciiProjection(t *testing.T) {
	identity := Identity{
		InstallationID: "install-\u00e9",
		SessionID:      "session-0000",
		ThreadID:       "thread-0000",
		WindowID:       "window-0000",
		TurnID:         "turn-0000",
	}
	header, clientMetadata, err := identity.TurnMetadata()
	if err != nil {
		t.Fatalf("building the turn metadata: %v", err)
	}
	for _, char := range header {
		if char > 0x7e {
			t.Fatalf("the header is not ascii: %s", header)
		}
	}
	if !strings.Contains(header, `\u00e9`) {
		t.Fatalf("the non ascii byte was not escaped: %s", header)
	}
	if !strings.HasPrefix(header, `{"installation_id":`) || !strings.HasSuffix(header, `"request_kind":"turn"}`) {
		t.Fatalf("the key order changed: %s", header)
	}
	if clientMetadata[HeaderTurnMetadata] != header {
		t.Fatal("the body projection and the header projection disagree")
	}
	if clientMetadata["turn_id"] != "turn-0000" || clientMetadata[HeaderWindowID] != "window-0000" {
		t.Fatalf("client metadata is %v", clientMetadata)
	}
}

func TestEncodeKeepsOneCacheKeyForTheWholeSessionAndSendsNoBreakpoints(t *testing.T) {
	request := Request{
		Model:    "gpt-5.5-codex",
		Messages: hello(),
		Identity: Identity{SessionID: "session-0000", TurnID: "turn-0000"},
	}
	first, err := request.Encode(nil)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	request.Messages = append(request.Messages,
		llm.Message{Role: llm.RoleAssistant, Content: "one"},
		llm.Message{Role: llm.RoleUser, Content: "two"})
	request.Identity.TurnID = "turn-0001"
	second, err := request.Encode(nil)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	if decodeBody(t, first)["prompt_cache_key"] != "session-0000" ||
		decodeBody(t, second)["prompt_cache_key"] != "session-0000" {
		t.Fatalf("the cache key is not stable across steps: %s then %s", first, second)
	}
	if strings.Contains(string(second), "cache_control") {
		t.Fatalf("the responses endpoint has no cache breakpoint field: %s", second)
	}
}

func TestEncodeKeepsAnExplicitCacheKeyOverTheSessionID(t *testing.T) {
	body, err := Request{
		Model:          "gpt-5.5-codex",
		Messages:       hello(),
		PromptCacheKey: "pinned",
		Identity:       Identity{SessionID: "session-0000"},
	}.Encode(nil)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	if decodeBody(t, body)["prompt_cache_key"] != "pinned" {
		t.Fatalf("the pinned key was overwritten: %s", body)
	}
}

func TestTurnMetadataRefusesToExceedTheBackendCap(t *testing.T) {
	identity := Identity{InstallationID: strings.Repeat("x", TurnMetadataHeaderCap+1)}
	if _, _, err := identity.TurnMetadata(); err == nil {
		t.Fatal("a turn metadata header past the backend cap was accepted")
	}
}
