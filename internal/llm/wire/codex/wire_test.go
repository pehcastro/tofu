package codex

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"tofu/internal/konst"
	"tofu/internal/transport"
)

const statusOverloaded = 529

type capture struct {
	path   string
	header http.Header
	body   []byte
}

func serve(t *testing.T, token string, body string) (*Wire, *capture) {
	t.Helper()
	seen := &capture{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.path = r.URL.Path
		seen.header = r.Header.Clone()
		seen.body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)

	wire, err := New(Config{
		BaseURL:        server.URL + SubscriptionPath,
		Model:          "gpt-5.5-codex",
		Token:          func(context.Context) (string, error) { return token, nil },
		InstallationID: "install-0000",
		SessionID:      "session-0000",
	})
	if err != nil {
		t.Fatalf("building the wire: %v", err)
	}
	return wire, seen
}

func serveStatus(t *testing.T, status int, header map[string]string, body string) *Wire {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		for name, value := range header {
			w.Header().Set(name, value)
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)

	wire, err := New(Config{
		BaseURL: server.URL + SubscriptionPath,
		Model:   "gpt-5.5-codex",
		Token:   func(context.Context) (string, error) { return subscriptionToken(t), nil },
	})
	if err != nil {
		t.Fatalf("building the wire: %v", err)
	}
	return wire
}

func TestAskSendsTheSubscriptionShape(t *testing.T) {
	wire, seen := serve(t, subscriptionToken(t), sse(textItemAdded, textDelta, textItemDone, responseDone))
	result, dump, err := wire.Ask(context.Background(), Request{Messages: hello()})
	t.Logf("request dump:\n%s", dump)
	if err != nil {
		t.Fatalf("asking: %v", err)
	}
	if !dump.Subscription || dump.Plan != "pro" {
		t.Fatalf("the dump reports subscription %v plan %q", dump.Subscription, dump.Plan)
	}
	if result.Content != "ok" {
		t.Fatalf("result is %+v", result)
	}
	if seen.header.Get(HeaderAccountID) != "acct-0000" {
		t.Fatalf("the account header is %q", seen.header.Get(HeaderAccountID))
	}
	if seen.header.Get(HeaderAPIKey) != "" {
		t.Fatal("an x-api-key went out on the subscription branch")
	}
	var body map[string]any
	if err := json.Unmarshal(seen.body, &body); err != nil {
		t.Fatalf("the body is not json: %v", err)
	}
	metadata, _ := body["client_metadata"].(map[string]any)
	if metadata[HeaderInstallationID] != "install-0000" || metadata["session_id"] != "session-0000" {
		t.Fatalf("client metadata is %v", metadata)
	}
	if metadata[HeaderTurnMetadata] != seen.header.Get(HeaderTurnMetadata) {
		t.Fatal("the header and body turn metadata projections disagree")
	}
}

func TestAskOnAKeyCarriesNoIdentity(t *testing.T) {
	wire, seen := serve(t, "sk-proj-0000", sse(textItemAdded, textDelta, textItemDone, responseDone))
	_, dump, err := wire.Ask(context.Background(), Request{Messages: hello()})
	if err != nil {
		t.Fatalf("asking: %v", err)
	}
	if dump.Subscription {
		t.Fatal("a key took the subscription branch")
	}
	for _, name := range []string{HeaderAccountID, HeaderOriginator, HeaderVersion, HeaderTurnMetadata, HeaderRoutingHint} {
		if seen.header.Get(name) != "" {
			t.Fatalf("the key branch sent %s", name)
		}
	}
	var body map[string]any
	if err := json.Unmarshal(seen.body, &body); err != nil {
		t.Fatalf("the body is not json: %v", err)
	}
	if _, present := body["client_metadata"]; present {
		t.Fatal("the key branch sent client metadata")
	}
}

func TestEndpointFollowsTheBranch(t *testing.T) {
	wire, err := New(Config{Model: "m", Token: func(context.Context) (string, error) { return "", nil }})
	if err != nil {
		t.Fatalf("building the wire: %v", err)
	}
	if got := wire.endpoint(true); got != "https://chatgpt.com/backend-api/codex/responses" {
		t.Fatalf("the subscription endpoint is %q", got)
	}
	if got := wire.endpoint(false); got != "https://api.openai.com/v1/responses" {
		t.Fatalf("the key endpoint is %q", got)
	}
}

func TestAskReportsTheRefusedControls(t *testing.T) {
	wire, _ := serve(t, subscriptionToken(t), sse(textItemAdded, textDelta, textItemDone, responseDone))
	temperature := 0.7
	result, _, err := wire.Ask(context.Background(), Request{
		Messages: hello(),
		Sampling: Sampling{Temperature: &temperature},
	})
	if err != nil {
		t.Fatalf("asking: %v", err)
	}
	if len(result.Warnings) == 0 || !strings.Contains(result.Warnings[0], "temperature") {
		t.Fatalf("warnings are %v", result.Warnings)
	}
}

func TestQuotaRejectionIsARateLimit(t *testing.T) {
	wire := serveStatus(t, http.StatusTooManyRequests,
		map[string]string{"x-codex-primary-used-percent": "100"},
		`{"detail":"You've hit your usage limit."}`)
	_, _, err := wire.Ask(context.Background(), Request{Messages: hello()})
	if transport.KindOf(err) != transport.KindRateLimit {
		t.Fatalf("a 429 classified as %v: %v", transport.KindOf(err), err)
	}
	if transport.KindOf(err).Fatal() {
		t.Fatalf("a quota rejection is fatal: %v", err)
	}
}

func TestRequestTimeoutIsATimeoutTheWayTransportReadsIt(t *testing.T) {
	wire := serveStatus(t, http.StatusRequestTimeout, nil, `{"detail":"took too long"}`)
	_, _, err := wire.Ask(context.Background(), Request{Messages: hello()})
	if got := transport.KindOf(err); got != transport.KindTimeout {
		t.Fatalf("a 408 classified as %s: %v", got, err)
	}
}

func TestMissingTokenFailsClosed(t *testing.T) {
	wire, err := New(Config{Model: "m", Token: func(context.Context) (string, error) { return "", nil }})
	if err != nil {
		t.Fatalf("building the wire: %v", err)
	}
	if _, _, err := wire.Ask(context.Background(), Request{Messages: hello()}); transport.KindOf(err) != transport.KindMissingCredential {
		t.Fatalf("an empty token gave %v", err)
	}
}

func TestAnErrorBodyIsTruncatedToTheTransportDetailCap(t *testing.T) {
	wire := serveStatus(t, http.StatusInternalServerError, nil,
		strings.Repeat("e", konst.TransportErrorDetailBytes*4))
	_, _, err := wire.Ask(context.Background(), Request{Messages: hello()})
	var failure *transport.Error
	if !errors.As(err, &failure) {
		t.Fatalf("error is %v", err)
	}
	if len(failure.Detail) != konst.TransportErrorDetailBytes {
		t.Fatalf("the detail is %d bytes and the cap is %d", len(failure.Detail), konst.TransportErrorDetailBytes)
	}
}

func TestAnOverloadedSubscriptionAnswerIsRetriedAndTheStreamStillArrives(t *testing.T) {
	served := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		served++
		if served == 1 {
			w.WriteHeader(statusOverloaded)
			_, _ = io.WriteString(w, `{"detail":"overloaded"}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, sse(textItemAdded, textDelta, textItemDone, responseDone))
	}))
	t.Cleanup(server.Close)

	wire, err := New(Config{
		BaseURL:   server.URL + SubscriptionPath,
		Model:     "gpt-5.5-codex",
		Token:     func(context.Context) (string, error) { return subscriptionToken(t), nil },
		Transport: transport.Config{Retries: konst.TurnRetries, Backoff: time.Millisecond, MaxBackoff: time.Millisecond},
	})
	if err != nil {
		t.Fatalf("building the wire: %v", err)
	}
	result, _, err := wire.Ask(context.Background(), Request{Messages: hello()})
	if err != nil {
		t.Fatalf("one 529 ended the turn: %v", err)
	}
	if served != 2 {
		t.Fatalf("the stub saw %d requests, want the failure and one retry", served)
	}
	if result.Content != "ok" {
		t.Fatalf("result is %+v", result)
	}
}

func TestNewRefusesAnIncompleteConfig(t *testing.T) {
	if _, err := New(Config{Token: func(context.Context) (string, error) { return "t", nil }}); err == nil {
		t.Fatal("a wire with no model was built")
	}
	if _, err := New(Config{Model: "m"}); err == nil {
		t.Fatal("a wire with no token source was built")
	}
}
