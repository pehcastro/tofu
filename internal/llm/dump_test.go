package llm_test

import (
	"slices"
	"strings"
	"testing"

	"tofu/internal/llm"
	"tofu/internal/llm/wire/anthropic"
	"tofu/internal/llm/wire/codex"
	"tofu/internal/llm/wire/openrouter"
)

const (
	stubToken     = "stub-token-0000"
	stubAccount   = "acct-0000"
	stubSession   = "session-0000"
	stubThread    = "thread-0000"
	stubWindow    = "window-0000"
	stubTurn      = "turn-0000"
	stubInstall   = "install-0000"
	stubResidency = "eu"
)

func wireHeaders(t *testing.T) map[string][]llm.Header {
	t.Helper()
	metadata, _, err := codex.Identity{
		InstallationID: stubInstall,
		SessionID:      stubSession,
		ThreadID:       stubThread,
		WindowID:       stubWindow,
		TurnID:         stubTurn,
	}.TurnMetadata()
	if err != nil {
		t.Fatalf("building the codex turn metadata: %v", err)
	}
	return map[string][]llm.Header{
		"anthropic oauth": anthropic.Headers(anthropic.HeaderOptions{
			Token: stubToken, OAuth: true, Stream: true, AgentRequest: true, SessionID: stubSession,
		}),
		"anthropic key": anthropic.Headers(anthropic.HeaderOptions{Token: stubToken, Stream: true}),
		"codex subscription": codex.Headers(codex.HeaderOptions{
			Token:        stubToken,
			Subscription: true,
			Claims:       codex.Claims{AccountID: stubAccount, Residency: stubResidency},
			Model:        "gpt-5.5-codex",
			Identity: codex.Identity{
				InstallationID: stubInstall,
				SessionID:      stubSession,
				ThreadID:       stubThread,
				WindowID:       stubWindow,
				TurnID:         stubTurn,
			},
			TurnMetadata: metadata,
			TurnState:    stubSession,
		}),
		"codex key":  codex.Headers(codex.HeaderOptions{Token: stubToken}),
		"openrouter": openrouter.Headers(stubToken),
	}
}

func TestNoDumpPrintsAnIdentifierOrACredential(t *testing.T) {
	for wire, headers := range wireHeaders(t) {
		text := llm.Dump{
			Method:  "POST",
			URL:     "https://example.invalid/v1",
			Headers: headers,
			Body:    []byte(`{"model":"m"}`),
		}.String()
		never := []string{stubToken, stubAccount, stubSession, stubThread, stubWindow, stubTurn, stubInstall}
		for _, forbidden := range never {
			if strings.Contains(text, forbidden) {
				t.Fatalf("the %s dump prints %q:\n%s", wire, forbidden, text)
			}
		}
	}
}

func TestEveryHeaderLeftInClearIsOnTheSafeList(t *testing.T) {
	safe := llm.HeadersSafeInClear()
	for wire, headers := range wireHeaders(t) {
		for _, header := range headers {
			onList := slices.Contains(safe, strings.ToLower(header.Name))
			inClear := llm.RedactHeader(header.Name, header.Value) == header.Value
			if inClear != onList {
				t.Fatalf("%s sends %s in clear=%v while the safe list says %v", wire, header.Name, inClear, onList)
			}
		}
	}
}

func TestTheBodyIdentifiersAreHiddenToo(t *testing.T) {
	text := llm.Dump{
		Method:      "POST",
		URL:         "https://example.invalid/v1",
		Body:        []byte(`{"metadata":{"user_id":"{\"session_id\":\"` + stubSession + `\"}"}}`),
		Identifiers: []string{stubSession, ""},
	}.String()
	if strings.Contains(text, stubSession) {
		t.Fatalf("the body prints the session id:\n%s", text)
	}
	if !strings.Contains(text, llm.Redacted) {
		t.Fatalf("the body was not marked as redacted:\n%s", text)
	}
}
