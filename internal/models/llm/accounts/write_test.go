package cred

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func lockedStore(t *testing.T, expires time.Time) (*Manager, *Store, *atomic.Int32, *atomic.Int32) {
	t.Helper()
	var minted atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		n := minted.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  fmt.Sprintf("access-%d-from-%s", n, body["refresh_token"]),
			"refresh_token": fmt.Sprintf("refresh-%d", n),
			"expires_in":    3600,
		})
	}))
	t.Cleanup(server.Close)
	store, err := Open(filepath.Join(t.TempDir(), "credentials.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Save(Credential{Provider: ClaudeSub, Kind: KindOAuth, Access: "access-0", Refresh: "refresh-0", Expires: expires}, time.Now()); err != nil {
		t.Fatalf("save: %v", err)
	}
	manager := NewManager(store, Spec{Provider: ClaudeSub, ClientID: "test", TokenURL: server.URL, TokenBody: BodyJSON})
	var failing atomic.Int32
	manager.save = func(id int64, credential Credential, expected string, now time.Time) (bool, error) {
		if failing.Load() > 0 {
			failing.Add(-1)
			return false, errors.New("database is locked")
		}
		return store.UpdateIfRefreshMatches(id, credential, expected, now)
	}
	return manager, store, &minted, &failing
}

func storedRefresh(t *testing.T, store *Store) string {
	t.Helper()
	row, _, err := store.RowAt(ClaudeSub, time.Now())
	if err != nil {
		t.Fatalf("row: %v", err)
	}
	return row.Credential.Refresh
}

func TestAWriteThatFailsOnceIsRetried(t *testing.T) {
	manager, store, minted, failing := lockedStore(t, time.Now().Add(-time.Minute))
	failing.Store(1)

	token, err := manager.Access(context.Background())
	if err != nil || token != "access-1-from-refresh-0" {
		t.Fatalf("a refresh whose first write failed gave %q, %v", token, err)
	}
	if got := storedRefresh(t, store); got != "refresh-1" || minted.Load() != 1 {
		t.Fatalf("after a retried write the store holds %q after %d grants", got, minted.Load())
	}
}

func TestATokenNoWriteLandedIsKeptAndWrittenLater(t *testing.T) {
	manager, store, minted, failing := lockedStore(t, time.Now().Add(-time.Minute))
	failing.Store(1 << 20)

	token, err := manager.Access(context.Background())
	if err != nil || token != "access-1-from-refresh-0" {
		t.Fatalf("a refresh no write landed for gave %q, %v", token, err)
	}
	if got := storedRefresh(t, store); got != "refresh-0" {
		t.Fatalf("the store holds %q though every write failed", got)
	}
	failing.Store(0)
	token, err = manager.Access(context.Background())
	if err != nil || token != "access-1-from-refresh-0" || minted.Load() != 1 {
		t.Fatalf("the kept token gave %q, %v after %d grants, want it with no second grant", token, err, minted.Load())
	}
	if got := storedRefresh(t, store); got != "refresh-1" {
		t.Fatalf("the kept token was never written: the store holds %q", got)
	}
}

func TestAKeptTokenThatWentStaleRefreshesFromItsOwnRefreshToken(t *testing.T) {
	manager, store, minted, failing := lockedStore(t, time.Now().Add(-time.Minute))
	failing.Store(1 << 20)
	if _, err := manager.Access(context.Background()); err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	manager.now = func() time.Time { return time.Now().Add(2 * time.Hour) }

	token, err := manager.Access(context.Background())
	if err != nil || token != "access-2-from-refresh-1" {
		t.Fatalf("a stale kept token refreshed to %q, %v; want a grant on the kept refresh-1", token, err)
	}
	if row, _, _ := store.RowAt(ClaudeSub, time.Now()); row.DisabledCause != "" || row.Credential.Refresh != "refresh-0" {
		t.Fatalf("the store holds %q disabled %q while no write lands", row.Credential.Refresh, row.DisabledCause)
	}
	failing.Store(0)
	if _, err := manager.Access(context.Background()); err != nil || minted.Load() != 2 {
		t.Fatalf("flushing gave %v after %d grants", err, minted.Load())
	}
	if got := storedRefresh(t, store); got != "refresh-2" {
		t.Fatalf("the newest kept token was not the one written: the store holds %q", got)
	}
}
