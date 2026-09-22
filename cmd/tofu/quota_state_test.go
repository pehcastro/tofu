package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"tofu/internal/llm/cred"
	"tofu/internal/llm/quota"
)

const (
	firstStubAccount  = "not-a-real-account-a"
	secondStubAccount = "not-a-real-account-b"
	anthropicUsage    = `{"five_hour":{"utilization":25,"resets_at":"2026-09-21T18:00:00Z"}}`
)

func stubAccess(account string) string { return "not-a-real-token-" + account }

func usageStub(t *testing.T) (string, func() []string) {
	t.Helper()
	var guard sync.Mutex
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		guard.Lock()
		seen = append(seen, r.Header.Get("Authorization"))
		guard.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(anthropicUsage))
	}))
	t.Cleanup(server.Close)
	return server.URL, func() []string {
		guard.Lock()
		defer guard.Unlock()
		return append([]string(nil), seen...)
	}
}

func storeWithTwoAnthropicAccounts(t *testing.T, now time.Time) *cred.Store {
	t.Helper()
	store, err := cred.Open(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	for _, account := range []string{firstStubAccount, secondStubAccount} {
		credential := cred.Credential{
			Provider:   cred.Anthropic,
			Kind:       cred.KindOAuth,
			Access:     stubAccess(account),
			Refresh:    "not-a-real-refresh-" + account,
			Expires:    now.Add(time.Hour),
			Authorized: now,
		}
		credential.Identity.AccountID = account
		if err := store.Save(credential, now); err != nil {
			t.Fatalf("seeding an account: %v", err)
		}
	}
	return store
}

func TestTwoAccountsOnOneProviderAreBothPolled(t *testing.T) {
	now := time.Now()
	store := storeWithTwoAnthropicAccounts(t, now)
	rows, err := store.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	url, requests := usageStub(t)

	results, err := pollRows(context.Background(), store, rows,
		func() time.Time { return now }, map[quota.Provider]string{quota.Anthropic: url})
	if err != nil {
		t.Fatalf("pollRows: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("polling two accounts gave %d results, want 2", len(results))
	}
	for index, result := range results {
		if result.err != nil {
			t.Fatalf("account %d was not polled: %v", index, result.err)
		}
	}
	sent := requests()
	if len(sent) != 2 {
		t.Fatalf("the usage endpoint saw %d requests, want one per account", len(sent))
	}
	for index, account := range []string{firstStubAccount, secondStubAccount} {
		if !strings.HasSuffix(sent[index], stubAccess(account)) {
			t.Fatalf("request %d carried another row's credential, so one account was polled twice", index)
		}
	}
}

func TestAnUnusableAccountIsReportedRatherThanPolledOrDropped(t *testing.T) {
	now := time.Now()
	store := storeWithTwoAnthropicAccounts(t, now)
	rows, err := store.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if err := store.Disable(rows[0].ID, "set aside by hand", now); err != nil {
		t.Fatalf("disable: %v", err)
	}
	rows, err = store.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	url, requests := usageStub(t)

	results, err := pollRows(context.Background(), store, rows,
		func() time.Time { return now }, map[quota.Provider]string{quota.Anthropic: url})
	if err != nil {
		t.Fatalf("pollRows: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("a disabled account gave %d results, want it reported alongside the usable one", len(results))
	}
	if sent := requests(); len(sent) != 1 {
		t.Fatalf("the usage endpoint saw %d requests, want only the usable account", len(sent))
	}
	state := credentialState(results[0], now)
	if !strings.HasPrefix(state, "not polled, ") || !strings.Contains(state, "set aside by hand") {
		t.Fatalf("a disabled account reads %q, which does not say it is unwatched or why", state)
	}
	if credentialState(results[1], now) != usageServingState {
		t.Fatalf("the usable account reads %q", credentialState(results[1], now))
	}
}
