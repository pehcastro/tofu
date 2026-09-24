package wire_test

import (
	"context"
	"encoding/json"
	"errors"
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
	"tofu/internal/judge/jev/wire"
	"tofu/internal/judge/jev/wire/openrouter"
	"tofu/internal/judge/jev/wire/typesafe"
	"tofu/internal/konst"
	"tofu/internal/transport"
)

type route struct {
	name            string
	alias           string
	failOp          string
	servedRequestID bool
	answers         string
	build           string
	billsCost       bool
	liveVariable    string
	new             func(wire.Config) (*wire.Wire, error)
}

func routes() []route {
	return []route{
		{
			name:   openrouter.Name,
			alias:  openrouter.Alias,
			failOp: "openrouter.New",
			answers: `{"model":"typesafe/jev-1.13-20260917","answers":{
 "risk":{"type":"score","score":1.25,"probabilities":{"0":0.1,"1":0.6,"2":0.25,"3":0.05},"confidence":0.65},
 "approval":{"type":"noul","noul":0.11},
 "user_requested":{"type":"noul","noul":0.75},
 "act":{"type":"choice","choice":"read","probabilities":{"read":0.94,"write":0.03,"network":0.03},"confidence":0.93}},
 "usage":{"input_tokens":776,"output_tokens":69,"cost":0.000032592},"id":"gen-dec-1","provider":"TypeSafe"}`,
			build:        "typesafe/jev-1.13-20260917",
			billsCost:    true,
			liveVariable: jev.OpenRouterVariable,
			new:          openrouter.New,
		},
		{
			name:            typesafe.Name,
			alias:           typesafe.Alias,
			failOp:          "typesafe.New",
			servedRequestID: true,
			answers: `{"model":"jev-1.13.0","answers":{
 "risk":{"type":"score","score":1.25,"probabilities":{"0":0.1,"1":0.6,"2":0.25,"3":0.05},"confidence":0.65,
 "legend":{"0":"none","1":"reversible","2":"costly","3":"destructive"}},
 "approval":{"type":"noul","noul":0.11},
 "user_requested":{"type":"noul","noul":0.75},
 "act":{"type":"choice","choice":"read","probabilities":{"read":0.94,"write":0.03,"network":0.03},"confidence":0.93}},
 "usage":{"input_tokens":454,"output_tokens":73}}`,
			build:        "jev-1.13.0",
			liveVariable: jev.TypeSafeVariable,
			new:          typesafe.New,
		},
	}
}

func wireConfig(endpoint string) wire.Config {
	return wire.Config{
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

func TestEveryWireCarriesItsOwnNameAndTheSharedCeilings(t *testing.T) {
	for _, r := range routes() {
		t.Run(r.name, func(t *testing.T) {
			built, err := r.new(wireConfig("http://example.invalid"))
			if err != nil {
				t.Fatalf("building the wire: %v", err)
			}
			caps := built.Caps()
			if caps.Name != r.name {
				t.Fatalf("the caps name is %q, a person must be able to tell which wire billed them", caps.Name)
			}
			if built.Model() != r.alias {
				t.Fatalf("the alias is %q", built.Model())
			}
			if caps.MaxStateTokens != konst.JudgeStateTokenCeiling || caps.MaxRequestTokens != konst.JudgeRequestTokenCeiling {
				t.Fatalf("token ceilings are %d and %d", caps.MaxStateTokens, caps.MaxRequestTokens)
			}
			if caps.MaxRequestBytes != jev.EstimateBytes(konst.JudgeStateTokenCeiling) {
				t.Fatalf("byte cap is %d, expected the state ceiling converted through the estimator", caps.MaxRequestBytes)
			}
			if caps.MaxChoiceOptions != konst.ChoiceCeiling || caps.MaxScoreLevels != konst.JudgeScoreLevelCeiling {
				t.Fatalf("option ceiling %d, score level ceiling %d", caps.MaxChoiceOptions, caps.MaxScoreLevels)
			}
			for _, kind := range []jev.CriteriaKind{jev.CriteriaString, jev.CriteriaObject, jev.CriteriaNull} {
				if !caps.Accepts(kind) {
					t.Fatalf("bench-001 measured %s criteria accepted", kind)
				}
			}
		})
	}
}

func TestEveryWireRefusesAWireWithNoCredentialUnderItsOwnLabel(t *testing.T) {
	for _, r := range routes() {
		t.Run(r.name, func(t *testing.T) {
			config := wireConfig("http://example.invalid")
			config.Key = ""
			_, err := r.new(config)
			if transport.KindOf(err) != transport.KindMissingCredential {
				t.Fatalf("expected kind missing_credential, got %v", err)
			}
			var failure *transport.Error
			if !errors.As(err, &failure) || failure.Op != r.failOp {
				t.Fatalf("the failure label is %v, want %q", err, r.failOp)
			}
		})
	}
}

func TestEveryWirePostsTheKeyAndFollowsItsRequestIDRule(t *testing.T) {
	for _, r := range routes() {
		t.Run(r.name, func(t *testing.T) {
			var auth, contentType, sent string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				body, _ := io.ReadAll(req.Body)
				auth, contentType, sent = req.Header.Get("Authorization"), req.Header.Get("Content-Type"), string(body)
				w.Header().Set(typesafe.RequestIDHeader, "req_01a0c2d32b9f75b1")
				_, _ = w.Write([]byte(`{"model":"jev-1.13.0","answers":{"a":{"type":"noul","noul":0.5}},
 "usage":{"input_tokens":1,"output_tokens":1}}`))
			}))
			defer server.Close()

			built, err := r.new(wireConfig(server.URL))
			if err != nil {
				t.Fatalf("building the wire: %v", err)
			}
			raw, err := built.Post(context.Background(), []byte(`{"model":"x"}`))
			if err != nil {
				t.Fatalf("posting: %v", err)
			}
			if auth != "Bearer test-key" || contentType != "application/json" || sent != `{"model":"x"}` {
				t.Fatalf("authorization %q content type %q body %q", auth, contentType, sent)
			}
			if raw.Attempts != 1 {
				t.Fatalf("attempts %d", raw.Attempts)
			}
			if r.servedRequestID && raw.RequestID != "req_01a0c2d32b9f75b1" {
				t.Fatalf("request id is %q, want the served one", raw.RequestID)
			}
			if !r.servedRequestID && !strings.HasPrefix(raw.RequestID, "req-") {
				t.Fatalf("request id is %q, want the transport's own", raw.RequestID)
			}
		})
	}
}

func TestEveryWireValidatesTheAnswersThroughTheClient(t *testing.T) {
	for _, r := range routes() {
		t.Run(r.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(r.answers))
			}))
			defer server.Close()

			built, err := r.new(wireConfig(server.URL))
			if err != nil {
				t.Fatalf("building the wire: %v", err)
			}
			client, err := jev.NewClient(jev.Config{Wire: built})
			if err != nil {
				t.Fatalf("building the client: %v", err)
			}
			decision, err := client.Ask(context.Background(), gateBattery())
			if err != nil {
				t.Fatalf("asking: %v", err)
			}
			if decision.Build != r.build {
				t.Fatalf("the build is %q", decision.Build)
			}
			if decision.Answers["act"].Choice != "read" || decision.Answers["risk"].Score != 1.25 {
				t.Fatalf("the answers are %+v", decision.Answers)
			}
			if r.billsCost != (decision.Usage.Cost > 0) {
				t.Fatalf("the decision carries $%v and bills cost is %v", decision.Usage.Cost, r.billsCost)
			}
		})
	}
}

func TestEveryWireRefusesAnOversizeRequestBeforeTheCall(t *testing.T) {
	for _, r := range routes() {
		t.Run(r.name, func(t *testing.T) {
			var calls int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls++
				_, _ = w.Write([]byte(`{}`))
			}))
			defer server.Close()

			built, err := r.new(wireConfig(server.URL))
			if err != nil {
				t.Fatalf("building the wire: %v", err)
			}
			client, err := jev.NewClient(jev.Config{Wire: built})
			if err != nil {
				t.Fatalf("building the client: %v", err)
			}
			request := gateBattery()
			request.State = map[string]string{"blob": strings.Repeat("x", built.Caps().MaxRequestBytes)}
			if _, err = client.Ask(context.Background(), request); transport.KindOf(err) != transport.KindRequestTooLarge {
				t.Fatalf("expected kind request_too_large, got %v", err)
			}
			if calls != 0 {
				t.Fatalf("expected nothing to reach the server, it saw %d calls", calls)
			}
		})
	}
}

func TestEveryWireWaitsTheRetryAfterHeaderItIsHanded(t *testing.T) {
	for _, r := range routes() {
		t.Run(r.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
			built, err := r.new(config)
			if err != nil {
				t.Fatalf("building the wire: %v", err)
			}
			raw, err := built.Post(context.Background(), []byte(`{"model":"x"}`))
			if err != nil {
				t.Fatalf("the retry did not recover the call: %v", err)
			}
			if raw.Attempts != 2 || calls.Load() != 2 {
				t.Fatalf("attempts %d over %d calls, want a second attempt", raw.Attempts, calls.Load())
			}
			if len(waited) != 1 || waited[0] != 2*time.Second {
				t.Fatalf("the wire waited %v, want the 2s the retry-after header asked for", waited)
			}
		})
	}
}

func TestLiveGate(t *testing.T) {
	for _, r := range routes() {
		t.Run(r.name, func(t *testing.T) {
			if os.Getenv("TOFU_LIVE") != "1" {
				t.Skip("set TOFU_LIVE=1 to call the real route")
			}
			jev.AllowLiveCredential(t)
			key, err := jev.KeyFor("../../../../.env", r.liveVariable)
			if err != nil {
				t.Fatalf("no credential: %v", err)
			}
			config := wireConfig("")
			config.Key = key
			built, err := r.new(config)
			if err != nil {
				t.Fatalf("building the wire: %v", err)
			}
			client, err := jev.NewClient(jev.Config{Wire: built})
			if err != nil {
				t.Fatalf("building the client: %v", err)
			}

			request := gateBattery()
			body, err := request.Encode(built.Model())
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
			estimate := jev.EstimateTokens(len(body))
			t.Logf("build %s request id %s transport id %s attempts %d", decision.Build, decision.RequestID, decision.TransportID, decision.Attempts)
			t.Logf("request %d bytes, estimated %d input tokens, billed %d input tokens, %d output tokens, latency %d ms",
				len(body), estimate, decision.Usage.InputTokens, decision.Usage.OutputTokens, elapsed.Milliseconds())

			if decision.Build == r.alias {
				t.Fatal("the response reported the alias instead of a build id")
			}
			if estimate < decision.Usage.InputTokens {
				t.Fatalf("the guard estimate undershot the bill: estimated %d, billed %d, request id %s",
					estimate, decision.Usage.InputTokens, decision.RequestID)
			}
			if r.billsCost && decision.Usage.Cost <= 0 {
				t.Fatal("the response reported no cost")
			}
		})
	}
}
