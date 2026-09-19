package cred

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestLoginCompletesThePkceFlowAndCapturesIdentity(t *testing.T) {
	now := time.Now()
	challenge := make(chan string, 1)

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var params map[string]string
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &params); err != nil {
			t.Errorf("token body was not JSON: %v", err)
		}
		sent := <-challenge
		sum := sha256.Sum256([]byte(params["code_verifier"]))
		if got := base64.RawURLEncoding.EncodeToString(sum[:]); got != sent {
			t.Errorf("code_verifier does not hash to the challenge sent: %q vs %q", got, sent)
		}
		if params["grant_type"] != "authorization_code" || params["code"] != "the-code" {
			t.Errorf("token params = %v", params)
		}
		if params["state"] == "" {
			t.Error("the exchange carried no state")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "fresh-access",
			"refresh_token": "fresh-refresh",
			"expires_in":    3600,
			"account":       map[string]any{"uuid": storedAccount, "email_address": storedEmail},
		})
	}))
	defer tokenServer.Close()

	spec := testSpec(tokenServer.URL)
	spec.AuthorizeURL = "http://authorize.invalid/oauth/authorize"
	spec.CallbackPath = "/callback"
	spec.PortFallback = true
	spec.SendStateOnExchange = true

	credential, err := Login(context.Background(), LoginOptions{
		Spec: spec,
		Open: func(target string) error {
			parsed, err := url.Parse(target)
			if err != nil {
				return err
			}
			query := parsed.Query()
			challenge <- query.Get("code_challenge")
			if query.Get("code_challenge_method") != "S256" {
				t.Errorf("challenge method = %q", query.Get("code_challenge_method"))
			}
			redirect, err := url.Parse(query.Get("redirect_uri"))
			if err != nil {
				return err
			}
			redirect.RawQuery = url.Values{"code": {"the-code"}, "state": {query.Get("state")}}.Encode()
			response, err := http.Get(redirect.String())
			if err != nil {
				return err
			}
			return response.Body.Close()
		},
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if credential.Access != "fresh-access" || credential.Refresh != "fresh-refresh" {
		t.Error("the minted credential did not carry the tokens the endpoint returned")
	}
	if credential.Identity.AccountID != storedAccount || credential.Identity.Email != storedEmail {
		t.Errorf("identity = %+v", credential.Identity)
	}
	if !credential.Expires.Equal(now.Add(time.Hour)) {
		t.Errorf("expires = %v, want %v", credential.Expires, now.Add(time.Hour))
	}
}

func TestPastedRedirectYieldsTheCodeAndRefusesAnotherState(t *testing.T) {
	code, err := codeFromPaste("http://localhost:54545/callback?code=abc&state=state-1", "state-1")
	if err != nil || code != "abc" {
		t.Errorf("code = %q, err = %v", code, err)
	}
	if _, err := codeFromPaste("http://localhost:54545/callback?code=abc&state=other", "state-1"); err == nil {
		t.Error("a pasted redirect with another state was accepted")
	}
	if code, err := codeFromPaste("  raw-code#state-1  ", "state-1"); err != nil || code != "raw-code#state-1" {
		t.Errorf("a pasted bare code should pass through: %q %v", code, err)
	}
}

func TestSpecsCarryThePortsAndScopesTheProviderAllowlists(t *testing.T) {
	anthropic, err := Lookup("anthropic")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if anthropic.CallbackPort != 54545 || anthropic.CallbackPath != "/callback" || !anthropic.PortFallback {
		t.Errorf("anthropic callback = %d%s fallback=%v", anthropic.CallbackPort, anthropic.CallbackPath, anthropic.PortFallback)
	}
	if anthropic.ClientID != "9d1c250a-e61b-44d9-88ed-5944d1962f5e" {
		t.Errorf("anthropic client id = %q", anthropic.ClientID)
	}
	if anthropic.ExpirySkew != 5*time.Minute || anthropic.GrantLife != 30*24*time.Hour {
		t.Errorf("anthropic skew = %v grant life = %v", anthropic.ExpirySkew, anthropic.GrantLife)
	}
	codex, err := Lookup("codex")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if codex.CallbackPort != 1455 || codex.CallbackPath != "/auth/callback" || codex.PortFallback {
		t.Errorf("codex callback = %d%s fallback=%v", codex.CallbackPort, codex.CallbackPath, codex.PortFallback)
	}
	if _, err := Lookup("gemini"); err == nil {
		t.Error("an unknown provider resolved")
	}
}
