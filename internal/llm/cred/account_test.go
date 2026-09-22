package cred

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	firstAccount  = "account-a"
	secondAccount = "account-b"
)

func seedAccount(t *testing.T, store *Store, account string, at time.Time) int64 {
	t.Helper()
	err := store.Save(Credential{
		Provider:   Anthropic,
		Kind:       KindOAuth,
		Access:     storedAccess,
		Refresh:    storedRefresh,
		Expires:    at.Add(time.Hour),
		Identity:   Identity{AccountID: account},
		Authorized: at,
	}, at)
	if err != nil {
		t.Fatalf("seeding %s: %v", account, err)
	}
	rows, err := store.List()
	if err != nil {
		t.Fatalf("listing after seeding %s: %v", account, err)
	}
	for _, row := range rows {
		if row.Credential.Identity.AccountID == account {
			return row.ID
		}
	}
	t.Fatalf("seeding %s stored nothing", account)
	return 0
}

func TestADisabledFirstAccountDoesNotHideTheSecond(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	store := openStore(t, t.TempDir())
	first := seedAccount(t, store, firstAccount, now)
	second := seedAccount(t, store, secondAccount, now)
	if err := store.Disable(first, "oauth refresh failed: invalid_grant", now); err != nil {
		t.Fatalf("disable: %v", err)
	}

	row, found, err := store.RowAt(Anthropic, now)
	if err != nil || !found {
		t.Fatalf("RowAt = %v, %v", found, err)
	}
	if row.ID != second {
		t.Fatalf("a disabled row was chosen over the usable one")
	}
}

func TestAnExpiredGrantDoesNotHideTheSecondAccount(t *testing.T) {
	authorized := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	store := openStore(t, t.TempDir())
	seedAccount(t, store, firstAccount, authorized)
	second := seedAccount(t, store, secondAccount, authorized.Add(anthropicSpec().GrantLife))
	now := authorized.Add(anthropicSpec().GrantLife + time.Hour)

	row, found, err := store.RowAt(Anthropic, now)
	if err != nil || !found {
		t.Fatalf("RowAt = %v, %v", found, err)
	}
	if row.ID != second {
		t.Fatalf("a credential whose refresh grant expired was chosen over the usable one")
	}
}

func TestTwoUsableAccountsForOneProviderAreRefusedRatherThanOrdered(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	store := openStore(t, t.TempDir())
	first := seedAccount(t, store, firstAccount, now)
	second := seedAccount(t, store, secondAccount, now)

	row, found, err := store.RowAt(Anthropic, now)
	var refusal TwoAccounts
	if !errors.As(err, &refusal) || found {
		t.Fatalf("RowAt = %d, %v, %v, want a refusal naming both credentials", row.ID, found, err)
	}
	if len(refusal.IDs) != 2 || refusal.IDs[0] != first || refusal.IDs[1] != second {
		t.Fatalf("the refusal names %v, want both stored credentials", refusal.IDs)
	}
	firstTag := "#" + strconv.FormatInt(first, 10)
	secondTag := "#" + strconv.FormatInt(second, 10)
	if !strings.Contains(err.Error(), firstTag) || !strings.Contains(err.Error(), secondTag) {
		t.Fatalf("the refusal does not name both stored credentials: %v", err)
	}
	for _, secret := range []string{firstAccount, secondAccount, storedAccess, storedRefresh} {
		if strings.Contains(err.Error(), secret) {
			t.Fatal("the refusal names something identifying")
		}
	}
}
