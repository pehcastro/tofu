package cred

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	storedAccess  = "stored-access"
	storedRefresh = "stored-refresh"
	mintedAccess  = "minted-access"
	storedEmail   = "person@example.test"
	storedAccount = "account-uuid-1"
)

func openStore(t *testing.T, dir string) *Store {
	t.Helper()
	store, err := Open(filepath.Join(dir, storeFileName))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func tokenStub(t *testing.T, hits *atomic.Int64, status int, body any, delay time.Duration) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		time.Sleep(delay)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func mintedBody() map[string]any {
	return map[string]any{"access_token": mintedAccess, "refresh_token": "minted-refresh", "expires_in": 3600}
}

func testSpec(tokenURL string) Spec {
	return Spec{
		Provider:      Anthropic,
		ClientID:      "client-id",
		TokenURL:      tokenURL,
		TokenBody:     BodyJSON,
		AccountIDPath: "account.uuid",
		EmailPath:     "account.email_address",
	}
}

func seed(t *testing.T, store *Store, at time.Time, expires time.Time) {
	t.Helper()
	err := store.Save(Credential{
		Provider:   Anthropic,
		Kind:       KindOAuth,
		Access:     storedAccess,
		Refresh:    storedRefresh,
		Expires:    expires,
		Identity:   Identity{AccountID: storedAccount, Email: storedEmail},
		Authorized: at,
	}, at)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func managerAt(store *Store, spec Spec, at time.Time) *Manager {
	manager := NewManager(store, spec)
	manager.now = func() time.Time { return at }
	return manager
}

func TestAccessRefreshesInsideTheSkewAndNotOutsideIt(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name    string
		expires time.Time
		want    string
		hits    int64
	}{
		{"inside the skew", now.Add(refreshSkew / 2), mintedAccess, 1},
		{"outside the skew", now.Add(refreshSkew * 10), storedAccess, 0},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var hits atomic.Int64
			store := openStore(t, t.TempDir())
			seed(t, store, now, testCase.expires)
			manager := managerAt(store, testSpec(tokenStub(t, &hits, http.StatusOK, mintedBody(), 0)), now)

			access, err := manager.Access(context.Background())
			if err != nil {
				t.Fatalf("access: %v", err)
			}
			if access != testCase.want {
				t.Errorf("access = %q, want %q", access, testCase.want)
			}
			if hits.Load() != testCase.hits {
				t.Errorf("token endpoint calls = %d, want %d", hits.Load(), testCase.hits)
			}
		})
	}
}

func TestConcurrentRefreshesCallTheTokenEndpointOnce(t *testing.T) {
	now := time.Now()
	dir := t.TempDir()
	var hits atomic.Int64
	url := tokenStub(t, &hits, http.StatusOK, mintedBody(), 50*time.Millisecond)

	store := openStore(t, dir)
	seed(t, store, now, now.Add(refreshSkew/2))

	managers := []*Manager{
		managerAt(store, testSpec(url), now),
		managerAt(store, testSpec(url), now),
		managerAt(openStore(t, dir), testSpec(url), now),
	}
	var group sync.WaitGroup
	start := make(chan struct{})
	results := make([]string, len(managers))
	errs := make([]error, len(managers))
	for index, manager := range managers {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			results[index], errs[index] = manager.Access(context.Background())
		}()
	}
	close(start)
	group.Wait()

	for index := range managers {
		if errs[index] != nil {
			t.Fatalf("access %d: %v", index, errs[index])
		}
		if results[index] != mintedAccess {
			t.Errorf("access %d = %q, want %q", index, results[index], mintedAccess)
		}
	}
	if hits.Load() != 1 {
		t.Errorf("token endpoint calls = %d, want 1", hits.Load())
	}
}

func TestDefinitiveRefreshFailureDisablesTheRowAndTransientDoesNot(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name     string
		status   int
		body     map[string]any
		disabled string
	}{
		{"definitive", http.StatusBadRequest, map[string]any{"error": "invalid_grant"}, "invalid_grant"},
		{"transient", http.StatusServiceUnavailable, map[string]any{"error": "service unavailable"}, ""},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var hits atomic.Int64
			store := openStore(t, t.TempDir())
			seed(t, store, now, now.Add(refreshSkew/2))
			manager := managerAt(store, testSpec(tokenStub(t, &hits, testCase.status, testCase.body, 0)), now)

			if _, err := manager.Access(context.Background()); err == nil {
				t.Fatal("access succeeded, want a refresh failure")
			}
			row, found, err := store.Row(Anthropic)
			if err != nil || !found {
				t.Fatalf("row: %v found=%v", err, found)
			}
			if testCase.disabled == "" {
				if row.DisabledCause != "" {
					t.Errorf("disabled cause = %q, want the row left alone", row.DisabledCause)
				}
				return
			}
			if !strings.Contains(row.DisabledCause, testCase.disabled) {
				t.Errorf("disabled cause = %q, want it to record %q", row.DisabledCause, testCase.disabled)
			}
		})
	}
}

func TestRefreshKeepsTheIdentityCapturedAtLogin(t *testing.T) {
	now := time.Now()
	var hits atomic.Int64
	store := openStore(t, t.TempDir())
	seed(t, store, now, now.Add(refreshSkew/2))
	body := mintedBody()
	body["account"] = map[string]any{"uuid": "other-account", "email_address": "someone-else@example.test"}
	manager := managerAt(store, testSpec(tokenStub(t, &hits, http.StatusOK, body, 0)), now)

	if _, err := manager.Access(context.Background()); err != nil {
		t.Fatalf("access: %v", err)
	}
	row, found, err := store.Row(Anthropic)
	if err != nil || !found {
		t.Fatalf("row: %v found=%v", err, found)
	}
	want := Identity{AccountID: storedAccount, Email: storedEmail}
	if row.Credential.Identity != want {
		t.Errorf("identity = %+v, want the identity captured at login %+v", row.Credential.Identity, want)
	}
	if row.Credential.Access != mintedAccess {
		t.Errorf("the refresh did not replace the access token")
	}
}

func TestDefinitiveFailureClassification(t *testing.T) {
	cases := []struct {
		message string
		want    bool
	}{
		{"token endpoint answered 400: {\"error\":\"invalid_grant\"}", true},
		{"token endpoint answered 400: refresh token expired", true},
		{"token endpoint answered 401: unauthorized", true},
		{"token endpoint answered 401: rate limit, retry later", false},
		{"token endpoint answered 503: unavailable", false},
		{"cred: token request failed: dial tcp: connection refused", false},
	}
	for _, testCase := range cases {
		if got := definitiveFailure(testCase.message); got != testCase.want {
			t.Errorf("definitiveFailure(%q) = %v, want %v", testCase.message, got, testCase.want)
		}
	}
}
