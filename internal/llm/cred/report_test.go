package cred

import (
	"strings"
	"testing"
	"time"
)

func TestReportNamesProviderKindAndExpiryAndNothingIdentifying(t *testing.T) {
	authorized := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	line := Report([]Row{{
		ID: 1,
		Credential: Credential{
			Provider:   ClaudeSub,
			Kind:       KindOAuth,
			Access:     "sk-ant-oat-secret",
			Refresh:    "refresh-secret",
			Expires:    authorized.Add(8 * time.Hour),
			Identity:   Identity{AccountID: storedAccount, Email: storedEmail, OrgName: "Some Org"},
			Authorized: authorized,
		},
	}}, authorized)
	for _, want := range []string{string(ClaudeSub), "oauth", "expires 2026-09-18T20:00:00Z", "re-login by 2026-10-18"} {
		if !strings.Contains(line, want) {
			t.Errorf("report %q is missing %q", line, want)
		}
	}
	for _, secret := range []string{"sk-ant-oat-secret", "refresh-secret", storedEmail, storedAccount, "Some Org"} {
		if strings.Contains(line, secret) {
			t.Fatalf("report leaked %q", secret)
		}
	}
}

func TestReportSaysNoneAndNamesADisabledRow(t *testing.T) {
	if got := Report(nil, time.Now()); got != "none" {
		t.Errorf("Report(nil) = %q, want none", got)
	}
	line := Report([]Row{{
		ID:            1,
		Credential:    Credential{Provider: ClaudeSub, Kind: KindOAuth},
		DisabledCause: "oauth refresh failed: token endpoint answered 400: invalid_grant",
	}}, time.Now())
	if !strings.Contains(line, "disabled: oauth refresh failed") || !strings.Contains(line, "invalid_grant") {
		t.Errorf("report = %q, want the disabled reason", line)
	}
}

func TestStoredRowsRoundTripThroughTheStore(t *testing.T) {
	now := time.Now()
	store := openStore(t, t.TempDir())
	seed(t, store, now, now.Add(time.Hour))

	rows, err := store.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].Credential.Access != storedAccess || rows[0].Credential.Kind != KindOAuth {
		t.Errorf("row = %+v", rows[0].Credential)
	}
	seed(t, store, now, now.Add(2*time.Hour))
	rows, err = store.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("a second login for the same account made %d rows, want 1", len(rows))
	}
}
