package cred

import (
	"context"
	"testing"
	"time"
)

const (
	firstAccount  = "account-a"
	secondAccount = "account-b"
)

func accountAccess(account string) string { return storedAccess + "-" + account }

func seedAccount(t *testing.T, store *Store, account string, at time.Time) int64 {
	t.Helper()
	err := store.Save(Credential{
		Provider:   ClaudeSub,
		Kind:       KindOAuth,
		Access:     accountAccess(account),
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

	row, found, err := store.RowAt(ClaudeSub, now)
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
	second := seedAccount(t, store, secondAccount, authorized.Add(claudeSubSpec().GrantLife))
	now := authorized.Add(claudeSubSpec().GrantLife + time.Hour)

	row, found, err := store.RowAt(ClaudeSub, now)
	if err != nil || !found {
		t.Fatalf("RowAt = %v, %v", found, err)
	}
	if row.ID != second {
		t.Fatalf("a credential whose refresh grant expired was chosen over the usable one")
	}
}

func TestAManagerNamingARowAnswersAndTheProviderSelectorTakesTheFirstUsableOne(t *testing.T) {
	now := time.Now()
	store := openStore(t, t.TempDir())
	spec := testSpec("")
	for _, account := range []string{firstAccount, secondAccount} {
		id := seedAccount(t, store, account, now)
		manager := NewAccountManager(store, spec, id)
		access, err := manager.Access(context.Background())
		if err != nil {
			t.Fatalf("a manager naming a stored row was refused: %v", err)
		}
		if access != accountAccess(account) {
			t.Fatal("a manager naming a row resolved a different row")
		}
	}
	access, err := NewManager(store, spec).Access(context.Background())
	if err != nil {
		t.Fatalf("the provider selector refused while two rows are usable: %v", err)
	}
	if access != accountAccess(firstAccount) {
		t.Fatal("the provider selector took neither stored row in order, and it knows no window to choose by")
	}
}

func TestTwoUsableAccountsForOneProviderAreOrderedRatherThanRefused(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	store := openStore(t, t.TempDir())
	first := seedAccount(t, store, firstAccount, now)
	seedAccount(t, store, secondAccount, now)

	row, found, err := store.RowAt(ClaudeSub, now)
	if err != nil || !found {
		t.Fatalf("RowAt = %v, %v, want the first usable row rather than a refusal", found, err)
	}
	if row.ID != first {
		t.Fatalf("RowAt chose #%d, want the first usable row: headroom is read by the picker, not here", row.ID)
	}
}
