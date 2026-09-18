package openrouter

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"boji/internal/judge/jev"
	"boji/internal/konst"
	"boji/internal/llm"
	"boji/internal/transport"
)

const liveAttemptMillis = 30000

func wireConfig(endpoint string) Config {
	return Config{
		Endpoint: endpoint,
		Model:    "anthropic/claude-fable-5-1",
		Key:      "test-key",
		Transport: transport.Config{
			AttemptTimeout: time.Duration(konst.JudgeTimeoutMillis) * time.Millisecond,
			Retries:        konst.JudgeRetries,
			Backoff:        50 * time.Millisecond,
			MaxBackoff:     500 * time.Millisecond,
			Concurrency:    1,
		},
	}
}

func TestNewRefusesAWireWithNoCredential(t *testing.T) {
	config := wireConfig("http://example.invalid")
	config.Key = ""
	_, err := New(config)
	if transport.KindOf(err) != transport.KindMissingCredential {
		t.Fatalf("expected kind missing_credential, got %v", err)
	}
}

func TestNewRefusesAWireWithNoModel(t *testing.T) {
	config := wireConfig("http://example.invalid")
	config.Model = ""
	_, err := New(config)
	if transport.KindOf(err) != transport.KindBadRequest {
		t.Fatalf("expected kind bad_request, got %v", err)
	}
}

func TestPostCarriesTheKeyAndReturnsTheBody(t *testing.T) {
	var auth, contentType, sent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		auth, contentType, sent = r.Header.Get("Authorization"), r.Header.Get("Content-Type"), string(body)
		_, _ = w.Write([]byte(`{"id":"gen-1","model":"m","choices":[{"finish_reason":"stop","message":{"content":"ok"}}]}`))
	}))
	defer server.Close()

	wire, err := New(wireConfig(server.URL))
	if err != nil {
		t.Fatalf("building the wire: %v", err)
	}
	raw, err := wire.Post(context.Background(), []byte(`{"model":"x"}`))
	if err != nil {
		t.Fatalf("posting: %v", err)
	}
	if auth != "Bearer test-key" {
		t.Fatalf("authorization was %q", auth)
	}
	if contentType != "application/json" {
		t.Fatalf("content type was %q", contentType)
	}
	if sent != `{"model":"x"}` {
		t.Fatalf("body was %q", sent)
	}
	if !strings.HasPrefix(raw.RequestID, "req-") || raw.Attempts != 1 {
		t.Fatalf("raw is %+v", raw)
	}
}

func TestAskOnARecordedRefusalIsADistinctOutcomeAndIsNotRetried(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		_, _ = w.Write([]byte(`{"id":"gen-refusal","model":"anthropic/claude-fable-5.1","choices":[
{"finish_reason":"content_filter","message":{"content":"","refusal":"I can't help with that request."}}],
"usage":{"prompt_tokens":41,"completion_tokens":0,"cost":0.0000014}}`))
	}))
	defer server.Close()

	wire, err := New(wireConfig(server.URL))
	if err != nil {
		t.Fatalf("building the wire: %v", err)
	}
	client, err := llm.NewClient(wire)
	if err != nil {
		t.Fatalf("building the client: %v", err)
	}

	decision, err := client.Ask(context.Background(), llm.Request{
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: "what does `ls -la` do, one sentence"},
		},
	})
	if err != nil {
		t.Fatalf("asking: %v", err)
	}
	if decision.Outcome != llm.OutcomeRefusal {
		t.Fatalf("expected outcome refusal, got %s", decision.Outcome)
	}
	if decision.Refusal != "I can't help with that request." {
		t.Fatalf("refusal text is %q", decision.Refusal)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("expected exactly one call, the client retried a refusal: %d calls", calls)
	}
}

func TestAskHonoursThePerAttemptTimeoutAndRetries(t *testing.T) {
	release := make(chan struct{})
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			<-release
			return
		}
		_, _ = w.Write([]byte(`{"id":"gen-2","model":"m","choices":[{"finish_reason":"stop","message":{"content":"ok"}}]}`))
	}))
	defer server.Close()
	defer close(release)

	config := wireConfig(server.URL)
	config.Transport.AttemptTimeout = 80 * time.Millisecond
	config.Transport.Retries = 1
	config.Transport.Backoff = 10 * time.Millisecond
	wire, err := New(config)
	if err != nil {
		t.Fatalf("building the wire: %v", err)
	}
	raw, err := wire.Post(context.Background(), []byte(`{}`))
	if err != nil {
		t.Fatalf("expected the retry to succeed, got %v", err)
	}
	if raw.Attempts != 2 {
		t.Fatalf("expected two attempts, got %d", raw.Attempts)
	}
}

func liveClient(t *testing.T) *llm.Client {
	t.Helper()
	if os.Getenv("BOJI_LIVE") != "1" {
		t.Skip("set BOJI_LIVE=1 to call the real route")
	}
	key, err := jev.Key("../../../../.env")
	if err != nil {
		t.Fatalf("no credential: %v", err)
	}
	config := wireConfig("")
	config.Key = key
	config.Transport.AttemptTimeout = liveAttemptMillis * time.Millisecond
	wire, err := New(config)
	if err != nil {
		t.Fatalf("building the wire: %v", err)
	}
	client, err := llm.NewClient(wire)
	if err != nil {
		t.Fatalf("building the client: %v", err)
	}
	return client
}

func TestLiveCompletion(t *testing.T) {
	client := liveClient(t)

	decision, err := client.Ask(context.Background(), llm.Request{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "reply with the single word: pong"}},
	})
	if err != nil {
		t.Fatalf("the live call failed: %v", err)
	}
	if decision.Outcome != llm.OutcomeMessage {
		t.Fatalf("expected a message outcome, got %s, refusal %q", decision.Outcome, decision.Refusal)
	}

	t.Logf("model %s", decision.Build)
	t.Logf("input tokens %d output tokens %d", decision.Usage.InputTokens, decision.Usage.OutputTokens)
	t.Logf("cost $%.9f", decision.Usage.Cost)
	t.Logf("request id %s transport id %s", decision.RequestID, decision.TransportID)
	t.Logf("content %q", decision.Content)

	if decision.Usage.Cost <= 0 {
		t.Fatal("the response reported no cost")
	}
	if decision.RequestID == "" {
		t.Fatal("the response reported no request id")
	}
}

func TestLiveToolCall(t *testing.T) {
	client := liveClient(t)

	request := llm.Request{
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: "what is the weather in Lisbon right now. use the get_weather tool."},
		},
		Tools: []llm.Tool{
			{
				Name:        "get_weather",
				Description: "returns the current weather for a named place",
				Parameters: map[string]any{
					"type":       "object",
					"properties": map[string]any{"place": map[string]any{"type": "string"}},
					"required":   []string{"place"},
				},
			},
		},
	}
	decision, err := client.Ask(context.Background(), request)
	if err != nil {
		t.Fatalf("the live call failed: %v", err)
	}
	if decision.Outcome != llm.OutcomeToolCalls {
		t.Fatalf("expected a tool call outcome, got %s, content %q, refusal %q", decision.Outcome, decision.Content, decision.Refusal)
	}
	if len(decision.ToolCalls) == 0 {
		t.Fatal("expected at least one tool call")
	}

	call := decision.ToolCalls[0]
	var arguments map[string]any
	if err := json.Unmarshal(call.Arguments, &arguments); err != nil {
		t.Fatalf("the tool call arguments did not parse: %v", err)
	}

	t.Logf("model %s", decision.Build)
	t.Logf("tool call id %s name %s arguments %s", call.ID, call.Name, call.Arguments)
	t.Logf("parsed arguments %+v", arguments)
	t.Logf("cost $%.9f request id %s", decision.Usage.Cost, decision.RequestID)
}
