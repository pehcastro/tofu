package cred

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const (
	codexAccountOne = "codex-account-one"
	codexAccountTwo = "codex-account-two"
	codexEmailOne   = "one@example.test"
	codexEmailTwo   = "two@example.test"
)

func codexJWT(t *testing.T, account, email string) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		codexAuthClaim: map[string]any{"chatgpt_account_id": account},
		"email":        email,
	})
	if err != nil {
		t.Fatalf("encoding the fabricated claims: %v", err)
	}
	segment := base64.RawURLEncoding.EncodeToString
	return segment([]byte(`{"alg":"none"}`)) + "." + segment(payload) + ".not-a-signature"
}

func codexLogin(t *testing.T, account, email string) Credential {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  codexJWT(t, account, ""),
			"refresh_token": "refresh-" + account,
			"id_token":      codexJWT(t, account, email),
			"expires_in":    3600,
		})
	}))
	t.Cleanup(server.Close)
	spec := codexSubSpec()
	spec.TokenURL = server.URL
	credential, err := exchange(context.Background(), server.Client(), spec,
		"the-code", "", "http://127.0.0.1:1455/auth/callback", "the-verifier", time.Now())
	if err != nil {
		t.Fatalf("the codex exchange failed: %v", err)
	}
	return credential
}

func TestACodexLoginCarriesTheAccountTheIDTokenNames(t *testing.T) {
	credential := codexLogin(t, codexAccountOne, codexEmailOne)
	if credential.Identity.AccountID != codexAccountOne {
		t.Fatal("a codex credential carries no account id after login")
	}
	if credential.Identity.Email != codexEmailOne {
		t.Fatal("a codex credential carries no email after login")
	}
}

func TestTwoCodexLoginsOnDifferentAccountsAreTwoRows(t *testing.T) {
	store := openStore(t, t.TempDir())
	now := time.Now()
	for _, credential := range []Credential{
		codexLogin(t, codexAccountOne, codexEmailOne),
		codexLogin(t, codexAccountTwo, codexEmailTwo),
	} {
		if err := store.Save(credential, now); err != nil {
			t.Fatalf("saving a codex credential: %v", err)
		}
	}
	rows, err := store.List()
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("two codex logins on different accounts stored %d rows", len(rows))
	}
	if rows[0].Credential.Identity.AccountID == rows[1].Credential.Identity.AccountID {
		t.Fatal("both stored rows carry the same account")
	}
}

func TestACodexRowStoredWithNoKeyIsRekeyedWhenTheStoreOpens(t *testing.T) {
	dir := t.TempDir()
	store := openStore(t, dir)
	now := time.Now()
	data, err := json.Marshal(Credential{
		Provider:   CodexSub,
		Kind:       KindOAuth,
		Access:     codexJWT(t, codexAccountOne, ""),
		Refresh:    "refresh-already-on-disk",
		Expires:    now.Add(time.Hour),
		Authorized: now,
	})
	if err != nil {
		t.Fatalf("encoding the row already on disk: %v", err)
	}
	if _, err := store.db.Exec(
		`INSERT INTO credentials (provider, kind, account_key, data, created_at, updated_at)
		VALUES (?, ?, '', ?, ?, ?)`,
		string(CodexSub), KindOAuth, string(data), now.UnixMilli(), now.UnixMilli()); err != nil {
		t.Fatalf("seeding the row already on disk: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("closing: %v", err)
	}

	reopened := openStore(t, dir)
	rows, err := reopened.List()
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("reopening left %d rows, want the one that was there", len(rows))
	}
	if rows[0].Credential.Identity.AccountID != codexAccountOne {
		t.Fatal("the row already on disk was not given the account its access token names")
	}
	if rows[0].Credential.Refresh != "refresh-already-on-disk" {
		t.Fatal("re-keying replaced the tokens the earlier login left")
	}

	if err := reopened.Save(codexLogin(t, codexAccountOne, codexEmailOne), now); err != nil {
		t.Fatalf("logging in again on the account already stored: %v", err)
	}
	rows, err = reopened.List()
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("logging in again on the account already stored left %d rows", len(rows))
	}
}
