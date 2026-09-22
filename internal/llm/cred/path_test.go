package cred

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"tofu/internal/sys"
)

func homeHolding(t *testing.T, dirs ...string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(home, dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

func TestTheCredentialStoreResolvesUnderTheNewDirectory(t *testing.T) {
	home := homeHolding(t, sys.StateDirName, sys.LegacyStateDirName)
	path, err := Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if want := filepath.Join(home, sys.StateDirName, storeFileName); path != want {
		t.Fatalf("the store resolves to %s, want %s", path, want)
	}
}

func TestTheOldCredentialStoreStillOpensWhileTheNewDirectoryIsAbsent(t *testing.T) {
	home := homeHolding(t, sys.LegacyStateDirName)
	path, err := Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if want := filepath.Join(home, sys.LegacyStateDirName, storeFileName); path != want {
		t.Fatalf("the store resolves to %s, want %s", path, want)
	}

	written, err := Open(path)
	if err != nil {
		t.Fatalf("opening the old store: %v", err)
	}
	credential := Credential{Provider: ClaudeSub, Kind: KindOAuth, Access: "not-a-real-token"}
	credential.Identity.AccountID = "account-1"
	if err := written.Save(credential, time.Now()); err != nil {
		t.Fatalf("saving into the old store: %v", err)
	}
	if err := written.Close(); err != nil {
		t.Fatalf("closing the old store: %v", err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatalf("reopening the old store: %v", err)
	}
	defer func() { _ = store.Close() }()
	rows, err := store.List()
	if err != nil {
		t.Fatalf("listing the old store: %v", err)
	}
	if len(rows) != 1 || rows[0].Credential.Identity.AccountID != "account-1" {
		t.Fatalf("the old store read %d rows, want the one that was saved", len(rows))
	}
}
