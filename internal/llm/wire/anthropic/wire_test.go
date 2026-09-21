package anthropic

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/transport"
)

const stubOAuthToken = "sk-ant-oat01-not-a-real-token"

func stubToken(token string, called *int) TokenSource {
	return func(context.Context) (string, error) {
		*called++
		return token, nil
	}
}

func TestIsOfficialBaseURLRefusesALookalikeHost(t *testing.T) {
	official := []string{"", "https://api.anthropic.com", "https://api.anthropic.com/", "https://api.anthropic.com/v1"}
	for _, url := range official {
		if !IsOfficialBaseURL(url) {
			t.Fatalf("%q was read as not official", url)
		}
	}
	lookalikes := []string{
		"https://api.anthropic.com.evil.com",
		"https://api.anthropic.com.evil.com/v1/messages",
		"https://api.anthropic.company",
		"http://api.anthropic.com",
		"https://evil.com/https://api.anthropic.com/",
	}
	for _, url := range lookalikes {
		if IsOfficialBaseURL(url) {
			t.Fatalf("%q was accepted as the official host; a prefix comparison hands it a subscription token", url)
		}
	}
}

func TestNewRefusesALookalikeHostBeforeAnyCredentialIsAttached(t *testing.T) {
	called := 0
	_, err := New(Config{
		BaseURL: "https://api.anthropic.com.evil.com",
		Model:   "claude-opus-4-1-20250805",
		Token:   stubToken(stubOAuthToken, &called),
	})
	if transport.KindOf(err) != transport.KindMissingCredential {
		t.Fatalf("error is %v", err)
	}
	if called != 0 {
		t.Fatalf("the token source was read %d times before the host check", called)
	}
	if strings.Contains(err.Error(), stubOAuthToken) {
		t.Fatal("the error carries the token")
	}
}

func TestNewAllowsANonOfficialHostOnlyForADeclaredProxy(t *testing.T) {
	config := Config{BaseURL: "https://gateway.internal/anthropic", Model: "m", Token: stubToken("k", new(int))}
	if _, err := New(config); err == nil {
		t.Fatal("a non-official host was accepted without the proxy flag")
	}
	config.Proxy = true
	if _, err := New(config); err != nil {
		t.Fatalf("a declared proxy was refused: %v", err)
	}
}

func liveLikeServer(t *testing.T, events string, seen *string, seenHeaders *http.Header) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		*seen = string(body)
		*seenHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, events)
	}))
	t.Cleanup(server.Close)
	return server
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

	wire, err := New(Config{BaseURL: server.URL, Model: "m", Token: stubToken(stubOAuthToken, new(int)), Proxy: true})
	if err != nil {
		t.Fatalf("building the wire: %v", err)
	}
	return wire
}

func textTurnEvents() string {
	return sseText(eventMessageStart, eventTextStart, eventTextDelta, eventTextStop, eventStopTurn, eventStop)
}

func TestAskPutsThePatchedCheckValueOnTheWire(t *testing.T) {
	var sent string
	var headers http.Header
	server := liveLikeServer(t, textTurnEvents(), &sent, &headers)

	wire, err := New(Config{BaseURL: server.URL, Model: "claude-opus-4-1-20250805",
		Token: stubToken(stubOAuthToken, new(int)), Proxy: true})
	if err != nil {
		t.Fatalf("building the wire: %v", err)
	}
	result, dump, err := wire.Ask(context.Background(), Request{Messages: []llm.Message{{Role: llm.RoleUser, Content: "say ok"}}})
	if err != nil {
		t.Fatalf("asking: %v", err)
	}
	if result.Content != "hi" {
		t.Fatalf("result is %+v", result)
	}
	if dump.Attestation != AttestationPatched {
		t.Fatalf("attestation is %s", dump.Attestation)
	}

	if strings.Contains(sent, BillingCheckPlaceholder) {
		t.Fatalf("the placeholder reached the wire: %s", sent)
	}
	if sent != string(dump.Body) {
		t.Fatal("the dump is not the bytes that went out")
	}

	at := strings.Index(sent, "cch=") + len("cch=")
	onTheWire := sent[at : at+BillingCheckHexChars]
	withPlaceholder := sent[:at] + "00000" + sent[at+BillingCheckHexChars:]
	if want := BillingCheckValue([]byte(withPlaceholder)); onTheWire != want {
		t.Fatalf("the wire carries cch=%s; the body with the placeholder hashes to %s", onTheWire, want)
	}
}

func TestAskSendsAnUnattestedRequestWhenTheAnchorIsMissing(t *testing.T) {
	var sent string
	var headers http.Header
	server := liveLikeServer(t, textTurnEvents(), &sent, &headers)

	wire, err := New(Config{BaseURL: server.URL, Model: "claude-opus-4-1-20250805",
		Token: stubToken(stubOAuthToken, new(int)), Proxy: true})
	if err != nil {
		t.Fatalf("building the wire: %v", err)
	}
	request := Request{
		System:   []string{BillingHeaderPrefix + " " + strings.Repeat("p", BillingCheckAnchorWindow+1) + " cch=00000;"},
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "say ok"}},
	}
	result, dump, err := wire.Ask(context.Background(), request)
	if err != nil {
		t.Fatalf("a missing anchor failed the request instead of warning: %v", err)
	}
	if dump.Attestation != AttestationUnanchored {
		t.Fatalf("attestation is %s", dump.Attestation)
	}
	if len(result.Warnings) == 0 || !strings.Contains(result.Warnings[0], "unattested") {
		t.Fatalf("warnings are %v", result.Warnings)
	}
	if !strings.Contains(sent, BillingCheckPlaceholder) {
		t.Fatalf("an unanchored body was patched anyway: %s", sent)
	}
	if result.Content != "hi" {
		t.Fatal("the request was not sent")
	}
}

func TestAskCarriesTheClaudeCodeHeaderSetAndTheOAuthQuery(t *testing.T) {
	var sent string
	var headers http.Header
	server := liveLikeServer(t, textTurnEvents(), &sent, &headers)

	wire, err := New(Config{BaseURL: server.URL, Model: "claude-opus-4-1-20250805",
		Token: stubToken(stubOAuthToken, new(int)), Proxy: true, SessionID: "session-1"})
	if err != nil {
		t.Fatalf("building the wire: %v", err)
	}
	_, dump, err := wire.Ask(context.Background(), Request{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "say ok"}},
		Tools:    []llm.Tool{{Name: "probe"}},
	})
	if err != nil {
		t.Fatalf("asking: %v", err)
	}

	t.Logf("request dump:\n%s", dump)
	if !strings.HasSuffix(dump.URL, MessagesPath+OAuthQuery) {
		t.Fatalf("url is %s", dump.URL)
	}
	want := map[string]string{
		"Accept":                   "application/json",
		"Content-Type":             "application/json",
		"User-Agent":               ClaudeCodeUserAgent,
		"X-Claude-Code-Session-Id": "session-1",
		"X-Stainless-Lang":         "js",
		"X-Stainless-Runtime":      "node",
		"X-Stainless-Timeout":      "600",
		"anthropic-version":        AnthropicAPIVersion,
		"x-app":                    "cli",
		"Connection":               "keep-alive",
		"Accept-Encoding":          "gzip, deflate, br, zstd",
		"anthropic-dangerous-direct-browser-access": "true",
	}
	for name, value := range want {
		if headers.Get(name) != value {
			t.Fatalf("%s was %q, want %q", name, headers.Get(name), value)
		}
	}
	if headers.Get("X-Api-Key") != "" {
		t.Fatal("an oauth request carried x-api-key")
	}
	if headers.Get("Authorization") != "Bearer "+stubOAuthToken {
		t.Fatal("the oauth token is not the bearer")
	}
	beta := headers.Get("anthropic-beta")
	for _, required := range ClaudeCodeAgentBetas(false) {
		if !strings.Contains(beta, required) {
			t.Fatalf("the beta list %q is missing %q", beta, required)
		}
	}
	if strings.Contains(beta, NeverAdvertisedBeta) {
		t.Fatal("the long context beta must never be advertised on a subscription")
	}
}

func TestASessionIDDrawnForTheCallerIsNotPrintedInTheDump(t *testing.T) {
	var sent string
	var headers http.Header
	server := liveLikeServer(t, textTurnEvents(), &sent, &headers)

	wire, err := New(Config{BaseURL: server.URL, Model: "claude-opus-4-1-20250805",
		Token: stubToken(stubOAuthToken, new(int)), Proxy: true})
	if err != nil {
		t.Fatalf("building the wire: %v", err)
	}
	_, dump, err := wire.Ask(context.Background(), Request{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "say ok"}},
	})
	if err != nil {
		t.Fatalf("asking: %v", err)
	}

	drawn := encodedUserID(t, []byte(sent)).SessionID
	if drawn == "" {
		t.Fatal("the request carries no session id at all")
	}
	if strings.Contains(dump.String(), drawn) {
		t.Fatalf("the dump prints the session id drawn for the caller:\n%s", dump)
	}
}

func TestTheRequestCarriesTheSessionIDAndTheDumpDoesNot(t *testing.T) {
	var sent string
	var headers http.Header
	server := liveLikeServer(t, textTurnEvents(), &sent, &headers)

	wire, err := New(Config{BaseURL: server.URL, Model: "claude-opus-4-1-20250805",
		Token: stubToken(stubOAuthToken, new(int)), Proxy: true, SessionID: "session-0000"})
	if err != nil {
		t.Fatalf("building the wire: %v", err)
	}
	_, dump, err := wire.Ask(context.Background(), Request{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "say ok"}},
	})
	if err != nil {
		t.Fatalf("asking: %v", err)
	}

	if headers.Get("X-Claude-Code-Session-Id") != "session-0000" {
		t.Fatalf("the request lost the session header: %q", headers.Get("X-Claude-Code-Session-Id"))
	}
	if strings.Contains(dump.String(), "session-0000") {
		t.Fatalf("the dump prints the session id:\n%s", dump)
	}
}

func TestDumpRedactsTheCredential(t *testing.T) {
	dump := Dump{Dump: llm.Dump{
		Method:  http.MethodPost,
		URL:     OfficialBaseURL + MessagesPath + OAuthQuery,
		Headers: Headers(HeaderOptions{Token: stubOAuthToken, OAuth: true, Stream: true, AgentRequest: true}),
		Body:    []byte(`{"model":"m"}`),
	}}
	text := dump.String()
	if strings.Contains(text, stubOAuthToken) {
		t.Fatal("the dump carries the token")
	}
	if !strings.Contains(text, "Authorization: Bearer "+llm.Redacted) {
		t.Fatalf("dump is %s", text)
	}
}

func TestAskRefusesANonStreamingEncodingItCannotRead(t *testing.T) {
	wire := serveStatus(t, http.StatusOK, map[string]string{"Content-Encoding": "zstd"}, "binary")
	_, _, err := wire.Ask(context.Background(), Request{Messages: []llm.Message{{Role: llm.RoleUser, Content: "x"}}})
	if err == nil || !strings.Contains(err.Error(), "zstd") {
		t.Fatalf("error is %v", err)
	}
}

func TestAskMapsEveryErrorStatusTheWayTransportDoes(t *testing.T) {
	for status, want := range map[int]transport.Kind{
		408: transport.KindTimeout,
		429: transport.KindRateLimit,
	} {
		wire := serveStatus(t, status, nil, `{"error":{"message":"no"}}`)
		_, _, err := wire.Ask(context.Background(), Request{Messages: []llm.Message{{Role: llm.RoleUser, Content: "x"}}})
		if got := transport.KindOf(err); got != want {
			t.Errorf("status %d is %s, expected %s", status, got, want)
		}
	}
}

func TestAPIKeyBranchCarriesNoClaudeCodeFingerprint(t *testing.T) {
	headers := Headers(HeaderOptions{Token: "sk-ant-api03-key", Stream: true})
	for _, header := range headers {
		if strings.HasPrefix(header.Name, "X-Stainless") || header.Name == "User-Agent" {
			t.Fatalf("an api key request carried %s", header.Name)
		}
		if header.Name == "Authorization" {
			t.Fatal("an api key request must authenticate with x-api-key")
		}
	}
	found := false
	for _, header := range headers {
		if header.Name == "Accept" && header.Value == "text/event-stream" {
			found = true
		}
	}
	if !found {
		t.Fatal("a streaming api key request accepts text/event-stream")
	}
}

func TestBetaHeaderDeduplicatesAndPreservesOrder(t *testing.T) {
	got := BetaHeader([]string{"a", "b", " a "}, []string{"b", "c", ""})
	if got != "a,b,c" {
		t.Fatalf("beta header is %q", got)
	}
}

func TestAnErrorBodyIsTruncatedToTheTransportDetailCap(t *testing.T) {
	wire := serveStatus(t, http.StatusInternalServerError, nil,
		strings.Repeat("e", konst.TransportErrorDetailBytes*4))
	_, _, err := wire.Ask(context.Background(), Request{Messages: []llm.Message{{Role: llm.RoleUser, Content: "x"}}})
	var failure *transport.Error
	if !errors.As(err, &failure) {
		t.Fatalf("error is %v", err)
	}
	if len(failure.Detail) != konst.TransportErrorDetailBytes {
		t.Fatalf("the detail is %d bytes and the cap is %d", len(failure.Detail), konst.TransportErrorDetailBytes)
	}
}

func TestIsOAuthTokenReadsTheTokenItself(t *testing.T) {
	if !IsOAuthToken(stubOAuthToken) || IsOAuthToken("sk-ant-api03-key") {
		t.Fatal("oauth detection is not the sk-ant-oat substring")
	}
}

const statusOverloaded = 529

func failingThenStreaming(t *testing.T, status int, served *int) *Wire {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		*served++
		if *served == 1 {
			w.WriteHeader(status)
			_, _ = io.WriteString(w, `{"type":"error","error":{"type":"overloaded_error"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, textTurnEvents())
	}))
	t.Cleanup(server.Close)

	wire, err := New(Config{
		BaseURL:   server.URL,
		Model:     "m",
		Proxy:     true,
		Token:     stubToken(stubOAuthToken, new(int)),
		Transport: transport.Config{Retries: konst.TurnRetries, Backoff: time.Millisecond, MaxBackoff: time.Millisecond},
	})
	if err != nil {
		t.Fatalf("building the wire: %v", err)
	}
	return wire
}

func TestAnOverloadedSubscriptionAnswerIsRetriedAndTheStreamStillArrives(t *testing.T) {
	served := 0
	wire := failingThenStreaming(t, statusOverloaded, &served)
	result, _, err := wire.Ask(context.Background(), Request{Messages: []llm.Message{{Role: llm.RoleUser, Content: "say ok"}}})
	if err != nil {
		t.Fatalf("one 529 ended the turn: %v", err)
	}
	if served != 2 {
		t.Fatalf("the stub saw %d requests, want the failure and one retry", served)
	}
	if result.Content != "hi" {
		t.Fatalf("result is %+v", result)
	}
}

func TestARefusedSubscriptionRequestIsNotRetried(t *testing.T) {
	served := 0
	wire := failingThenStreaming(t, http.StatusBadRequest, &served)
	if _, _, err := wire.Ask(context.Background(), Request{Messages: []llm.Message{{Role: llm.RoleUser, Content: "say ok"}}}); err == nil {
		t.Fatal("a 400 was read as an answer")
	}
	if served != 1 {
		t.Fatalf("the stub saw %d requests, and it would have answered the second: a refusal sent again is the same refusal", served)
	}
}
