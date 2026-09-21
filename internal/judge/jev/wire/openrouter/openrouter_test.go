package openrouter

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"tofu/internal/judge/jev"
	"tofu/internal/konst"
	"tofu/internal/transport"
)

func wireConfig(endpoint string) Config {
	return Config{
		Endpoint: endpoint,
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

func gateBattery() jev.Request {
	return jev.Request{
		State: map[string]any{"tool": "bash", "input": "ls -la", "context": "the user asked what is in the folder"},
		Questions: []jev.Question{
			{ID: "risk", Kind: jev.QuestionScore, Instructions: "how much damage could running `tool` with `input` do",
				Levels: []string{"none", "reversible", "costly", "destructive"}},
			{ID: "approval", Kind: jev.QuestionNoul, Instructions: "should a person approve `input` before it runs",
				True: "a person should be asked first", False: "it can run without asking"},
			{ID: "user_requested", Kind: jev.QuestionNoul, Instructions: "did the user ask for `input` in `context`",
				True: "the user asked for it", False: "the user did not ask for it"},
			{ID: "act", Kind: jev.QuestionChoice, Instructions: "what does `input` do",
				Options: []jev.Option{
					{Name: "read", Criteria: "it only reads"},
					{Name: "write", Criteria: "it changes something"},
					{Name: "network", Criteria: "it reaches the network"},
				}},
		},
	}
}

func TestCapsDeclareTheMeasuredLimits(t *testing.T) {
	wire, err := New(wireConfig("http://example.invalid"))
	if err != nil {
		t.Fatalf("building the wire: %v", err)
	}
	caps := wire.Caps()
	if caps.MaxStateTokens != konst.JudgeStateTokenCeiling {
		t.Fatalf("state token ceiling is %d", caps.MaxStateTokens)
	}
	if caps.MaxRequestTokens != konst.JudgeRequestTokenCeiling {
		t.Fatalf("request token ceiling is %d", caps.MaxRequestTokens)
	}
	if caps.MaxRequestBytes != jev.EstimateBytes(konst.JudgeStateTokenCeiling) {
		t.Fatalf("byte cap is %d, expected the state ceiling converted through the estimator", caps.MaxRequestBytes)
	}
	if caps.MaxChoiceOptions != konst.ChoiceCeiling {
		t.Fatalf("option ceiling is %d", caps.MaxChoiceOptions)
	}
	if caps.MaxScoreLevels != konst.JudgeScoreLevelCeiling {
		t.Fatalf("score level ceiling is %d", caps.MaxScoreLevels)
	}
	for _, kind := range []jev.CriteriaKind{jev.CriteriaString, jev.CriteriaObject, jev.CriteriaNull} {
		if !caps.Accepts(kind) {
			t.Fatalf("bench-001 measured %s criteria accepted", kind)
		}
	}
	if wire.Model() != Alias {
		t.Fatalf("the alias is %q", wire.Model())
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

func TestPostCarriesTheKeyAndReturnsTheBody(t *testing.T) {
	var auth, contentType, sent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		auth, contentType, sent = r.Header.Get("Authorization"), r.Header.Get("Content-Type"), string(body)
		_, _ = w.Write([]byte(`{"model":"typesafe/jev-1.13-20260917","answers":{"a":{"type":"noul","noul":0.5}},
 "usage":{"input_tokens":1,"output_tokens":1,"cost":0.000001},"id":"gen-dec-1","provider":"TypeSafe"}`))
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

func TestClientOverTheWireValidatesTheAnswers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"model":"typesafe/jev-1.13-20260917","answers":{
 "risk":{"type":"score","score":0,"probabilities":{"0":1,"1":0,"2":0,"3":0},"confidence":1},
 "approval":{"type":"noul","noul":0.11},
 "user_requested":{"type":"noul","noul":0.75},
 "act":{"type":"choice","choice":"read","probabilities":{"read":0.94,"write":0.03,"network":0.02},"confidence":0.93}},
 "usage":{"input_tokens":776,"output_tokens":69,"cost":0.000032592},"id":"gen-dec-1","provider":"TypeSafe"}`))
	}))
	defer server.Close()

	wire, err := New(wireConfig(server.URL))
	if err != nil {
		t.Fatalf("building the wire: %v", err)
	}
	client, err := jev.NewClient(jev.Config{Wire: wire})
	if err != nil {
		t.Fatalf("building the client: %v", err)
	}
	decision, err := client.Ask(context.Background(), gateBattery())
	if err != nil {
		t.Fatalf("asking: %v", err)
	}
	if decision.Build != "typesafe/jev-1.13-20260917" || decision.RequestID != "gen-dec-1" {
		t.Fatalf("decision is %+v", decision)
	}
	if decision.Answers["act"].Choice != "read" {
		t.Fatalf("the choice is %q", decision.Answers["act"].Choice)
	}
}

func TestClientOverTheWireRefusesAnOversizeRequest(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	wire, err := New(wireConfig(server.URL))
	if err != nil {
		t.Fatalf("building the wire: %v", err)
	}
	client, err := jev.NewClient(jev.Config{Wire: wire})
	if err != nil {
		t.Fatalf("building the client: %v", err)
	}
	request := gateBattery()
	request.State = map[string]string{"blob": strings.Repeat("x", wire.Caps().MaxRequestBytes)}
	_, err = client.Ask(context.Background(), request)
	if transport.KindOf(err) != transport.KindRequestTooLarge {
		t.Fatalf("expected kind request_too_large, got %v", err)
	}
	if calls != 0 {
		t.Fatalf("expected nothing to reach the server, it saw %d calls", calls)
	}
}

func liveClient(t *testing.T) (*Wire, *jev.Client) {
	t.Helper()
	if os.Getenv("TOFU_LIVE") != "1" {
		t.Skip("set TOFU_LIVE=1 to call the real route")
	}
	jev.AllowLiveCredential(t)
	key, err := jev.Key("../../../../../.env")
	if err != nil {
		t.Fatalf("no credential: %v", err)
	}
	config := wireConfig("")
	config.Key = key
	wire, err := New(config)
	if err != nil {
		t.Fatalf("building the wire: %v", err)
	}
	client, err := jev.NewClient(jev.Config{Wire: wire})
	if err != nil {
		t.Fatalf("building the client: %v", err)
	}
	return wire, client
}

func TestLiveGate(t *testing.T) {
	_, client := liveClient(t)

	start := time.Now()
	decision, err := client.Ask(context.Background(), gateBattery())
	if err != nil {
		t.Fatalf("the live call failed: %v", err)
	}
	elapsed := time.Since(start)

	ids := make([]string, 0, len(decision.Answers))
	for id := range decision.Answers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		answer := decision.Answers[id]
		distribution, _ := json.Marshal(answer.Probabilities)
		switch answer.Kind {
		case jev.QuestionNoul:
			t.Logf("answer %-14s noul %.2f", id, answer.Noul)
		case jev.QuestionChoice:
			t.Logf("answer %-14s choice %q confidence %.2f %s", id, answer.Choice, answer.Confidence, distribution)
		case jev.QuestionScore:
			t.Logf("answer %-14s score %.2f confidence %.2f %s", id, answer.Score, answer.Confidence, distribution)
		}
	}
	t.Logf("build %s provider %s", decision.Build, decision.Provider)
	t.Logf("request id %s transport id %s attempts %d", decision.RequestID, decision.TransportID, decision.Attempts)
	t.Logf("cost $%.9f input %d tokens output %d tokens", decision.Usage.Cost, decision.Usage.InputTokens, decision.Usage.OutputTokens)
	t.Logf("latency %d ms request %d bytes cost $%.9f", elapsed.Milliseconds(), decision.Bytes, decision.Usage.Cost)

	if decision.Build == Alias {
		t.Fatal("the response reported the alias instead of a build id")
	}
	if decision.Usage.Cost <= 0 {
		t.Fatal("the response reported no cost")
	}
}

func TestLiveTokenEstimateAgainstTheBilledUsage(t *testing.T) {
	wire, client := liveClient(t)

	request := gateBattery()
	body, err := request.Encode(wire.Model())
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	estimate := jev.EstimateTokens(len(body))

	decision, err := client.Ask(context.Background(), request)
	if err != nil {
		t.Fatalf("the live call failed: %v", err)
	}
	billed := decision.Usage.InputTokens
	errorPct := float64(estimate-billed) / float64(billed) * 100
	direction := "over"
	if estimate < billed {
		direction = "under"
	}

	t.Logf("request id %s", decision.RequestID)
	t.Logf("request %d bytes, estimated %d input tokens, billed %d input tokens, %s by %.1f%%", len(body), estimate, billed, direction, errorPct)

	if estimate < billed {
		t.Fatalf("the guard estimate undershot the bill: estimated %d, billed %d, request id %s", estimate, billed, decision.RequestID)
	}
}
