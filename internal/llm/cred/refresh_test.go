package cred_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"tofu/internal/llm/cred"
)

type tokenEndpoint struct {
	delay  time.Duration
	minted atomic.Int32
}

func (e *tokenEndpoint) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	minted := e.minted.Add(1)
	time.Sleep(e.delay)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token":  fmt.Sprintf("access-%d", minted),
		"refresh_token": fmt.Sprintf("refresh-%d", minted),
		"expires_in":    3600,
	})
}

func seeded(t *testing.T, tokenURL, access string, expires time.Time) (*cred.Store, cred.Spec) {
	t.Helper()
	store, err := cred.Open(filepath.Join(t.TempDir(), "credentials.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Save(cred.Credential{Provider: cred.ClaudeSub, Kind: cred.KindOAuth, Access: access,
		Refresh: "refresh-0", Expires: expires}, time.Now()); err != nil {
		t.Fatalf("save: %v", err)
	}
	return store, cred.Spec{Provider: cred.ClaudeSub, ClientID: "test", TokenURL: tokenURL, TokenBody: cred.BodyJSON}
}

func stored(t *testing.T, store *cred.Store) cred.Row {
	t.Helper()
	row, found, err := store.Row(cred.ClaudeSub)
	if err != nil || !found {
		t.Fatalf("row: found %v, %v", found, err)
	}
	return row
}

func TestARefreshTheCallerGaveUpOnStillStoresTheRotatedToken(t *testing.T) {
	endpoint := &tokenEndpoint{delay: 400 * time.Millisecond}
	server := httptest.NewServer(endpoint)
	t.Cleanup(server.Close)
	store, spec := seeded(t, server.URL, "access-0", time.Now().Add(-time.Minute))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := cred.NewManager(store, spec).Access(ctx); err == nil {
		t.Fatal("the caller cancelled at 50 ms and still got a token from a 400 ms grant")
	}
	time.Sleep(600 * time.Millisecond)
	if row := stored(t, store); row.Credential.Refresh != "refresh-1" || row.DisabledCause != "" {
		t.Fatalf("after the grant finished the store holds refresh %q, disabled %q", row.Credential.Refresh, row.DisabledCause)
	}
}

func TestACallerJoiningARefreshLeavesWhenItsOwnContextEnds(t *testing.T) {
	endpoint := &tokenEndpoint{delay: 500 * time.Millisecond}
	server := httptest.NewServer(endpoint)
	t.Cleanup(server.Close)
	store, spec := seeded(t, server.URL, "access-0", time.Now().Add(-time.Minute))
	manager := cred.NewManager(store, spec)

	go func() { _, _ = manager.Access(context.Background()) }()
	time.Sleep(50 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := manager.Access(ctx)
	if waited := time.Since(started); err == nil || waited > 300*time.Millisecond {
		t.Fatalf("the joining caller cancelled at 50 ms and returned after %s with %v", waited, err)
	}
	if minted := endpoint.minted.Load(); minted != 1 {
		t.Fatalf("two callers on one credential sent %d grants", minted)
	}
}

func TestAGrantThatNeverAnsweredDoesNotDisableTheCredential(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		connection, _, _ := w.(http.Hijacker).Hijack()
		_ = connection.Close()
	}))
	t.Cleanup(server.Close)
	store, spec := seeded(t, server.URL+"/oauth/401", "access-0", time.Now().Add(-time.Minute))

	if _, err := cred.NewManager(store, spec).Access(context.Background()); err == nil {
		t.Fatal("a dropped connection produced a token")
	}
	if row := stored(t, store); row.DisabledCause != "" {
		t.Fatalf("a connection the token endpoint dropped disabled the credential: %q", row.DisabledCause)
	}
}

func TestARejectedTokenIsRefreshedThoughTheClockCallsItFresh(t *testing.T) {
	endpoint := &tokenEndpoint{}
	server := httptest.NewServer(endpoint)
	t.Cleanup(server.Close)
	store, spec := seeded(t, server.URL, "access-0", time.Now().Add(time.Hour))

	token, err := cred.NewManager(store, spec).Token(context.Background(), "access-0")
	if err != nil || token != "access-1" || endpoint.minted.Load() != 1 {
		t.Fatalf("a rejected fresh token gave %q, %v, after %d grants", token, err, endpoint.minted.Load())
	}
}

func TestARejectionAPeerAlreadyRepairedSendsNoGrant(t *testing.T) {
	endpoint := &tokenEndpoint{}
	server := httptest.NewServer(endpoint)
	t.Cleanup(server.Close)
	store, spec := seeded(t, server.URL, "access-peer", time.Now().Add(time.Hour))

	token, err := cred.NewManager(store, spec).Token(context.Background(), "access-0")
	if err != nil || token != "access-peer" || endpoint.minted.Load() != 0 {
		t.Fatalf("a token a peer already replaced gave %q, %v, after %d grants", token, err, endpoint.minted.Load())
	}
}
