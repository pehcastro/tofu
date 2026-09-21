package typesafe

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"sync/atomic"
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

func TestCapsDeclareTheDocumentedLimits(t *testing.T) {
	wire, err := New(wireConfig("http://example.invalid"))
	if err != nil {
		t.Fatalf("building the wire: %v", err)
	}
	caps := wire.Caps()
	if caps.Name != Name {
		t.Fatalf("the caps name is %q", caps.Name)
	}
	if caps.MaxStateTokens != konst.JudgeStateTokenCeiling {
		t.Fatalf("state token ceiling is %d", caps.MaxStateTokens)
	}
	if caps.MaxRequestTokens != konst.JudgeRequestTokenCeiling {
		t.Fatalf("request token ceiling is %d", caps.MaxRequestTokens)
	}
	if caps.MaxChoiceOptions != konst.ChoiceCeiling {
		t.Fatalf("option ceiling is %d, the docs say 255 per choice", caps.MaxChoiceOptions)
	}
	if caps.MaxScoreLevels != konst.JudgeScoreLevelCeiling {
		t.Fatalf("score level ceiling is %d, the docs say 10", caps.MaxScoreLevels)
	}
	if wire.Model() != Alias {
		t.Fatalf("the alias is %q, the direct route takes %q", wire.Model(), Alias)
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

func TestPostCarriesTheKeyAndReportsTheServedRequestID(t *testing.T) {
	var auth, sent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		auth, sent = r.Header.Get("Authorization"), string(body)
		w.Header().Set(RequestIDHeader, "req_01a0c2d32b9f75b1")
		_, _ = w.Write([]byte(`{"model":"jev-1.13.0","answers":{"a":{"type":"noul","noul":0.5}},
 "usage":{"input_tokens":454,"output_tokens":73}}`))
	}))
	defer server.Close()

	wire, err := New(wireConfig(server.URL))
	if err != nil {
		t.Fatalf("building the wire: %v", err)
	}
	raw, err := wire.Post(context.Background(), []byte(`{"model":"jev-latest"}`))
	if err != nil {
		t.Fatalf("posting: %v", err)
	}
	if auth != "Bearer test-key" {
		t.Fatalf("authorization was %q", auth)
	}
	if sent != `{"model":"jev-latest"}` {
		t.Fatalf("body was %q", sent)
	}
	if raw.RequestID != "req_01a0c2d32b9f75b1" || raw.Attempts != 1 {
		t.Fatalf("raw is %+v, want the served request id", raw)
	}
}

func TestClientOverTheWireValidatesTheAnswers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"model":"jev-1.13.0","answers":{
 "risk":{"type":"score","score":1.25,"probabilities":{"0":0.1,"1":0.6,"2":0.25,"3":0.05},"confidence":0.65,
 "legend":{"0":"none","1":"reversible","2":"costly","3":"destructive"}},
 "approval":{"type":"noul","noul":0.11},
 "user_requested":{"type":"noul","noul":0.75},
 "act":{"type":"choice","choice":"read","probabilities":{"read":0.94,"write":0.03,"network":0.03},"confidence":0.93}},
 "usage":{"input_tokens":454,"output_tokens":73}}`))
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
	if decision.Build != "jev-1.13.0" {
		t.Fatalf("the build is %q", decision.Build)
	}
	if decision.Answers["act"].Choice != "read" || decision.Answers["risk"].Score != 1.25 {
		t.Fatalf("the answers are %+v", decision.Answers)
	}
	if decision.Usage.Cost != 0 {
		t.Fatalf("the direct route reports no cost, the decision carries $%v", decision.Usage.Cost)
	}
}

func TestATooManyRequestsWaitsTheHeaderItCarries(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"rate limit"}`))
			return
		}
		_, _ = w.Write([]byte(`{"model":"jev-1.13.0","answers":{"a":{"type":"noul","noul":0.5}},
 "usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer server.Close()

	var waited []time.Duration
	config := wireConfig(server.URL)
	config.Transport.Retries = 1
	config.Transport.Sleep = func(_ context.Context, wait time.Duration) error {
		waited = append(waited, wait)
		return nil
	}
	wire, err := New(config)
	if err != nil {
		t.Fatalf("building the wire: %v", err)
	}
	raw, err := wire.Post(context.Background(), []byte(`{"model":"jev-latest"}`))
	if err != nil {
		t.Fatalf("the retry did not recover the call: %v", err)
	}
	if raw.Attempts != 2 || calls.Load() != 2 {
		t.Fatalf("attempts %d over %d calls, want a second attempt", raw.Attempts, calls.Load())
	}
	if len(waited) != 1 || waited[0] != 2*time.Second {
		t.Fatalf("the wire waited %v, want the 2s the retry-after header asked for", waited)
	}
}

func liveClient(t *testing.T) (*Wire, *jev.Client) {
	t.Helper()
	if os.Getenv("TOFU_LIVE") != "1" {
		t.Skip("set TOFU_LIVE=1 to call the direct typesafe route")
	}
	jev.AllowLiveCredential(t)
	key, err := jev.KeyFor("../../../../../.env", jev.TypeSafeVariable)
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
	wire, client := liveClient(t)

	request := gateBattery()
	body, err := request.Encode(wire.Model())
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	start := time.Now()
	decision, err := client.Ask(context.Background(), request)
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
	t.Logf("build %s served id %s attempts %d", decision.Build, decision.TransportID, decision.Attempts)
	t.Logf("request %d bytes, %d input tokens, %d output tokens, latency %d ms",
		len(body), decision.Usage.InputTokens, decision.Usage.OutputTokens, elapsed.Milliseconds())
	t.Logf("cost $%.9f at $42 per billion input tokens", float64(decision.Usage.InputTokens)*DollarsPerInputToken)
	t.Logf("raw response: %s", strings.TrimSpace(string(decision.Raw)))

	if decision.Build == Alias {
		t.Fatal("the response reported the alias instead of a build id")
	}
}
