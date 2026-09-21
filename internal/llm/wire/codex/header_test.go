package codex

import (
	"slices"
	"strings"
	"testing"

	"tofu/internal/llm"
)

func headerNames(headers []Header) []string {
	names := make([]string, len(headers))
	for index, header := range headers {
		names[index] = header.Name
	}
	return names
}

func headerValue(headers []Header, name string) string {
	for _, header := range headers {
		if strings.EqualFold(header.Name, name) {
			return header.Value
		}
	}
	return ""
}

func testHeaderOptions() HeaderOptions {
	return HeaderOptions{
		Token:        "jwt-token",
		Subscription: true,
		Claims:       Claims{AccountID: "acct-0000", PlanType: "pro", Residency: "eu"},
		Model:        "gpt-5.5-codex",
		Identity: Identity{
			InstallationID: "install-0000",
			SessionID:      "session-0000",
			ThreadID:       "thread-0000",
			WindowID:       "window-0000",
			TurnID:         "turn-0000",
		},
		TurnMetadata: `{"installation_id":"install-0000"}`,
	}
}

func TestSubscriptionHeadersMatchTheSpec(t *testing.T) {
	headers := Headers(testHeaderOptions())
	want := []string{
		HeaderAuthorization,
		HeaderAccountID,
		HeaderRoutingHint,
		HeaderResidency,
		HeaderBeta,
		HeaderOriginator,
		HeaderVersion,
		HeaderUserAgent,
		HeaderConversationID,
		HeaderSessionID,
		HeaderClientRequestID,
		HeaderScopedSessionID,
		HeaderThreadID,
		HeaderWindowID,
		HeaderTurnMetadata,
		HeaderAccept,
		HeaderContentType,
	}
	if !slices.Equal(headerNames(headers), want) {
		t.Fatalf("headers are\n%v\nwant\n%v", headerNames(headers), want)
	}
	for name, value := range map[string]string{
		HeaderAuthorization: "Bearer jwt-token",
		HeaderAccountID:     "acct-0000",
		HeaderRoutingHint:   "model=gpt-5.5-codex",
		HeaderResidency:     "eu",
		HeaderBeta:          "responses=experimental",
		HeaderOriginator:    "tofu",
		HeaderVersion:       "0.153.0",
		HeaderAccept:        "text/event-stream",
		HeaderContentType:   "application/json",
	} {
		if got := headerValue(headers, name); got != value {
			t.Fatalf("%s is %q, want %q", name, got, value)
		}
	}
	if !strings.HasPrefix(headerValue(headers, HeaderUserAgent), UserAgentPrefix) {
		t.Fatalf("the user agent is %q", headerValue(headers, HeaderUserAgent))
	}
	if headerValue(headers, HeaderSessionID) != headerValue(headers, HeaderScopedSessionID) {
		t.Fatal("session_id and session-id disagree")
	}
	if headerValue(headers, HeaderInstallationID) != "" {
		t.Fatal("the installation id rides in the body, never in a header")
	}
}

func TestRoutingHintCarriesTheTier(t *testing.T) {
	options := testHeaderOptions()
	options.ServiceTier = "priority"
	if got := headerValue(Headers(options), HeaderRoutingHint); got != "model=gpt-5.5-codex;tier=priority" {
		t.Fatalf("the routing hint is %q", got)
	}
}

func TestKeyBranchCarriesNoSubscriptionHeader(t *testing.T) {
	options := testHeaderOptions()
	options.Subscription = false
	options.Token = "sk-proj-0000"
	headers := Headers(options)
	want := []string{HeaderAuthorization, HeaderAccept, HeaderContentType}
	if !slices.Equal(headerNames(headers), want) {
		t.Fatalf("the key branch sent %v, want %v", headerNames(headers), want)
	}
}

func TestDumpRedactsTheCredentialAndTheIdentity(t *testing.T) {
	dump := Dump{Dump: llm.Dump{
		Method:  "POST",
		URL:     SubscriptionBaseURL + SubscriptionPath,
		Headers: Headers(testHeaderOptions()),
		Body:    []byte(`{"model":"gpt-5.5-codex"}`),
	}}
	text := dump.String()
	for _, secret := range []string{"jwt-token", "acct-0000", "session-0000", "thread-0000", "window-0000", "install-0000"} {
		if strings.Contains(text, secret) {
			t.Fatalf("the dump leaks %q:\n%s", secret, text)
		}
	}
	for _, kept := range []string{"model=gpt-5.5-codex", "responses=experimental", "0.153.0", "eu"} {
		if !strings.Contains(text, kept) {
			t.Fatalf("the dump lost %q:\n%s", kept, text)
		}
	}
}
