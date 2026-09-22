package cred

import (
	"encoding/json"
	"testing"
	"time"
)

func seedRetiredRow(t *testing.T, store *Store, word, account string, at time.Time) {
	t.Helper()
	data, err := json.Marshal(Credential{
		Provider:   Provider(word),
		Kind:       KindOAuth,
		Access:     accountAccess(account),
		Refresh:    storedRefresh,
		Expires:    at.Add(time.Hour),
		Identity:   Identity{AccountID: account},
		Authorized: at,
	})
	if err != nil {
		t.Fatalf("encoding the row filed under %s: %v", word, err)
	}
	if _, err := store.db.Exec(
		`INSERT INTO credentials (provider, kind, account_key, data, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		word, KindOAuth, account, string(data), at.UnixMilli(), at.UnixMilli()); err != nil {
		t.Fatalf("seeding the row filed under %s: %v", word, err)
	}
}

func TestACredentialFiledUnderTheRetiredWordIsReadBackAndUsed(t *testing.T) {
	now := time.Now()
	dir := t.TempDir()
	store := openStore(t, dir)
	seedRetiredRow(t, store, retiredClaudeWord, firstAccount, now)
	seedRetiredRow(t, store, retiredCodexWord, secondAccount, now)
	if err := store.Close(); err != nil {
		t.Fatalf("closing: %v", err)
	}

	reopened := openStore(t, dir)
	rows, err := reopened.List()
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("reopening left %d rows, want the two that were there", len(rows))
	}
	for _, want := range []struct {
		source  Provider
		account string
	}{{ClaudeSub, firstAccount}, {CodexSub, secondAccount}} {
		row, found, err := reopened.RowAt(want.source, now)
		if err != nil || !found {
			t.Fatalf("RowAt(%s) = %v, %v", want.source, found, err)
		}
		if row.Credential.Identity.AccountID != want.account {
			t.Fatalf("RowAt(%s) chose account %q", want.source, row.Credential.Identity.AccountID)
		}
		if row.Credential.Provider != want.source {
			t.Fatalf("the row reads back as %q, want %q", row.Credential.Provider, want.source)
		}
		if row.Unusable(now) != "" {
			t.Fatalf("the row filed under the retired word is refused: %s", row.Unusable(now))
		}
	}
}

func TestReopeningTwiceLeavesTheRenamedRowsAloneAndLosesNoLogin(t *testing.T) {
	now := time.Now()
	dir := t.TempDir()
	store := openStore(t, dir)
	seedRetiredRow(t, store, retiredClaudeWord, firstAccount, now)
	if err := store.Close(); err != nil {
		t.Fatalf("closing: %v", err)
	}

	for reopen := range 3 {
		reopened := openStore(t, dir)
		rows, err := reopened.List()
		if err != nil {
			t.Fatalf("listing: %v", err)
		}
		if len(rows) != 1 {
			t.Fatalf("reopen %d left %d rows, want the one login that was stored", reopen, len(rows))
		}
		if rows[0].Credential.Access != accountAccess(firstAccount) {
			t.Fatalf("reopen %d changed what the login carries", reopen)
		}
		if err := reopened.Close(); err != nil {
			t.Fatalf("closing: %v", err)
		}
	}
}

func TestLookupAnswersToTheRetiredWordAndReturnsTheNewOne(t *testing.T) {
	for word, want := range map[string]Provider{
		retiredClaudeWord: ClaudeSub,
		retiredCodexWord:  CodexSub,
		string(ClaudeSub): ClaudeSub,
		string(CodexSub):  CodexSub,
	} {
		spec, err := Lookup(word)
		if err != nil {
			t.Fatalf("Lookup(%q): %v", word, err)
		}
		if spec.Provider != want {
			t.Fatalf("Lookup(%q) names %q, want %q", word, spec.Provider, want)
		}
	}
	if _, err := Lookup("openai"); err == nil {
		t.Fatal("a vendor that pays by key was accepted as a subscription")
	}
}
