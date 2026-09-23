package cred

import (
	"testing"
	"time"
)

func TestParseProviderRefusesAnUnknownValue(t *testing.T) {
	_, err := ParseProvider("gemini")
	if err == nil {
		t.Fatal("an unknown provider was accepted")
	}
	want := `cred: unknown provider "gemini", want claude-sub or codex-sub`
	if err.Error() != want {
		t.Fatalf("ParseProvider(\"gemini\") error = %q, want %q", err.Error(), want)
	}
}

func TestABadProviderRowIsDisabledRatherThanDropped(t *testing.T) {
	now := time.Now()
	dir := t.TempDir()
	store := openStore(t, dir)

	good := Credential{Provider: ClaudeSub, Kind: KindOAuth, Access: accountAccess(firstAccount)}
	good.Identity.AccountID = firstAccount
	if err := store.Save(good, now); err != nil {
		t.Fatalf("saving the good row: %v", err)
	}
	bad := Credential{Provider: Provider("gemini"), Kind: KindOAuth, Access: accountAccess(secondAccount)}
	bad.Identity.AccountID = secondAccount
	if err := store.Save(bad, now); err != nil {
		t.Fatalf("saving the bad row: %v", err)
	}

	rows, err := store.List()
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("List returned %d rows, want both the good row and the disabled one", len(rows))
	}
	if rows[0].Credential.Provider != ClaudeSub || rows[0].Unusable(now) != "" {
		t.Fatalf("the good row reads as %+v", rows[0])
	}
	want := `cred: unknown provider "gemini", want claude-sub or codex-sub`
	if rows[1].DisabledCause != want {
		t.Fatalf("the bad row carries cause %q, want %q", rows[1].DisabledCause, want)
	}
	if got := rows[1].Unusable(now); got != "disabled: "+want {
		t.Fatalf("the bad row reports as usable: %q", got)
	}
}
