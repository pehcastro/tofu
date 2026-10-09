package cred_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"tofu/internal/llm/cred"
)

type grantLog struct {
	mu     sync.Mutex
	sent   []string
	refuse map[string]refusal
	before func(refresh string)
}

type refusal struct {
	status int
	body   string
}

func (g *grantLog) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body map[string]string
	_ = json.NewDecoder(r.Body).Decode(&body)
	refresh := body["refresh_token"]
	g.mu.Lock()
	g.sent = append(g.sent, refresh)
	minted := len(g.sent)
	refused, refuses := g.refuse[refresh]
	before := g.before
	g.mu.Unlock()
	if before != nil {
		before(refresh)
	}
	if refuses {
		w.WriteHeader(refused.status)
		_, _ = w.Write([]byte(refused.body))
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token":  fmt.Sprintf("access-%d", minted),
		"refresh_token": fmt.Sprintf("refresh-%d", minted),
		"expires_in":    3600,
	})
}

func (g *grantLog) grants() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return slices.Clone(g.sent)
}

func keptStore(t *testing.T, endpoint http.Handler) *cred.Store {
	t.Helper()
	server := httptest.NewServer(endpoint)
	t.Cleanup(server.Close)
	t.Setenv(cred.ClaudeTokenURLVariable, server.URL)
	store, err := cred.Open(filepath.Join(t.TempDir(), "credentials.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func account(t *testing.T, store *cred.Store, key, refresh string, expires, refreshed time.Time) int64 {
	t.Helper()
	credential := cred.Credential{Provider: cred.ClaudeSub, Kind: cred.KindOAuth, Access: "access-" + key, Refresh: refresh,
		Expires: expires, Refreshed: refreshed, Authorized: time.Now().Add(-40 * 24 * time.Hour),
		Identity: cred.Identity{AccountID: key, Email: key + "@example.com"}}
	if err := store.Save(credential, time.Now()); err != nil {
		t.Fatalf("save: %v", err)
	}
	rows, err := store.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	return rows[len(rows)-1].ID
}

func rowOf(t *testing.T, store *cred.Store, id int64) cred.Row {
	t.Helper()
	row, found, err := store.RowByID(id)
	if err != nil || !found {
		t.Fatalf("row %d: found %v, %v", id, found, err)
	}
	return row
}

func sweep(t *testing.T, store *cred.Store) []string {
	t.Helper()
	var notes []string
	cred.NewKeeper(store, func(line string) { notes = append(notes, line) }).Sweep(context.Background(), time.Now())
	return notes
}

func TestTheSweepRefreshesAnAccountIdleForNineDays(t *testing.T) {
	endpoint := &grantLog{}
	store := keptStore(t, endpoint)
	now := time.Now()
	id := account(t, store, "idle", "refresh-0", now.Add(time.Hour), now.Add(-9*24*time.Hour))

	notes := sweep(t, store)
	row := rowOf(t, store, id)
	if got := endpoint.grants(); !slices.Equal(got, []string{"refresh-0"}) {
		t.Fatalf("an account idle for nine days sent grants %v, want one for refresh-0; notes %v", got, notes)
	}
	if row.Credential.Refresh != "refresh-1" || now.Sub(row.Credential.Refreshed) > time.Minute {
		t.Fatalf("after the sweep the row holds refresh %q refreshed %v", row.Credential.Refresh, row.Credential.Refreshed)
	}
	if cause := row.Unusable(now); cause != "" {
		t.Fatalf("an account authorized 40 days ago and refreshed now reads unusable: %s", cause)
	}
	if deadline, dated := row.ReloginBy(now); dated {
		t.Fatalf("an account the server gave no refresh expiry shows re-login by %v", deadline)
	}
}

func TestTheSweepRefreshesAnAccountWhoseTokenExpiresInFiveMinutes(t *testing.T) {
	endpoint := &grantLog{}
	store := keptStore(t, endpoint)
	now := time.Now()
	account(t, store, "soon", "refresh-0", now.Add(5*time.Minute), now.Add(-time.Hour))

	notes := sweep(t, store)
	if got := endpoint.grants(); !slices.Equal(got, []string{"refresh-0"}) {
		t.Fatalf("an access token five minutes from expiry sent grants %v; notes %v", got, notes)
	}
}

func TestARowWithNoRefreshStampIsDue(t *testing.T) {
	endpoint := &grantLog{}
	store := keptStore(t, endpoint)
	account(t, store, "legacy", "refresh-0", time.Now().Add(time.Hour), time.Time{})

	sweep(t, store)
	if got := endpoint.grants(); !slices.Equal(got, []string{"refresh-0"}) {
		t.Fatalf("a row written before refresh stamps sent grants %v, want one", got)
	}
}

func TestTheSweepLeavesFreshAndSetAsideAccountsAlone(t *testing.T) {
	endpoint := &grantLog{}
	store := keptStore(t, endpoint)
	now := time.Now()
	account(t, store, "fresh", "refresh-a", now.Add(time.Hour), now.Add(-time.Hour))
	aside := account(t, store, "aside", "refresh-b", now.Add(-time.Hour), now.Add(-9*24*time.Hour))
	if err := store.Disable(aside, "set aside by hand", now); err != nil {
		t.Fatalf("disable: %v", err)
	}

	sweep(t, store)
	if got := endpoint.grants(); len(got) != 0 {
		t.Fatalf("a fresh account and a set-aside one sent grants %v", got)
	}
}

func TestOneAccountsFailureDoesNotStopTheSweep(t *testing.T) {
	endpoint := &grantLog{refuse: map[string]refusal{"refresh-a": {http.StatusInternalServerError, "upstream down"}}}
	store := keptStore(t, endpoint)
	now := time.Now()
	first := account(t, store, "first", "refresh-a", now.Add(-time.Minute), now.Add(-time.Hour))
	second := account(t, store, "second", "refresh-b", now.Add(-time.Minute), now.Add(-time.Hour))

	notes := sweep(t, store)
	if got := endpoint.grants(); !slices.Equal(got, []string{"refresh-a", "refresh-b"}) {
		t.Fatalf("grants %v, want both accounts tried", got)
	}
	if row := rowOf(t, store, second); row.Credential.Refresh != "refresh-2" {
		t.Fatalf("the second account holds %q after the first one's 500", row.Credential.Refresh)
	}
	if row := rowOf(t, store, first); row.DisabledCause != "" {
		t.Fatalf("a 500 set the account aside: %q", row.DisabledCause)
	}
	if !slices.ContainsFunc(notes, func(note string) bool { return strings.Contains(note, "upstream down") }) {
		t.Fatalf("the sweep did not say why the first account failed: %v", notes)
	}
}

func peerWrites(t *testing.T, store *cred.Store, endpoint *grantLog, id int64, refresh string, expires time.Time) {
	t.Helper()
	endpoint.before = func(sent string) {
		if sent != "refresh-0" {
			return
		}
		peer := rowOf(t, store, id).Credential
		peer.Access, peer.Refresh, peer.Expires = "access-peer", refresh, expires
		if replaced, err := store.UpdateIfRefreshMatches(id, peer, "refresh-0", time.Now()); err != nil || !replaced {
			t.Errorf("the peer's write: replaced %v, %v", replaced, err)
		}
	}
}

func invalidGrant() refusal {
	return refusal{http.StatusBadRequest, `{"error":"invalid_grant","error_description":"refresh token not found"}`}
}

func TestARefusalAfterAPeerRotatedUsesThePeersToken(t *testing.T) {
	endpoint := &grantLog{refuse: map[string]refusal{"refresh-0": invalidGrant()}}
	store := keptStore(t, endpoint)
	id := account(t, store, "peer", "refresh-0", time.Now().Add(-time.Minute), time.Now().Add(-time.Hour))
	peerWrites(t, store, endpoint, id, "refresh-peer", time.Now().Add(time.Hour))

	token, err := cred.NewAccountManager(store, mustSpec(t), id).Access(context.Background())
	if err != nil || token != "access-peer" {
		t.Fatalf("after a peer rotated the token the refused refresh gave %q, %v", token, err)
	}
	if got := endpoint.grants(); !slices.Equal(got, []string{"refresh-0"}) {
		t.Fatalf("grants %v, want only the refused one", got)
	}
	if row := rowOf(t, store, id); row.DisabledCause != "" {
		t.Fatalf("a peer's rotation set the account aside: %q", row.DisabledCause)
	}
}

func TestARefusalAfterAPeerRotatedToAStaleTokenRetriesOnce(t *testing.T) {
	endpoint := &grantLog{refuse: map[string]refusal{"refresh-0": invalidGrant()}}
	store := keptStore(t, endpoint)
	id := account(t, store, "peer", "refresh-0", time.Now().Add(-time.Minute), time.Now().Add(-time.Hour))
	peerWrites(t, store, endpoint, id, "refresh-peer", time.Now().Add(-time.Minute))

	token, err := cred.NewAccountManager(store, mustSpec(t), id).Access(context.Background())
	if err != nil || token != "access-2" {
		t.Fatalf("a refused refresh after a stale peer rotation gave %q, %v", token, err)
	}
	if got := endpoint.grants(); !slices.Equal(got, []string{"refresh-0", "refresh-peer"}) {
		t.Fatalf("grants %v, want the refused one and one retry on the peer's token", got)
	}
}

func TestASecondRefusalAfterTheReloadSetsTheAccountAsideAndStops(t *testing.T) {
	endpoint := &grantLog{refuse: map[string]refusal{"refresh-0": invalidGrant(), "refresh-peer": invalidGrant()}}
	store := keptStore(t, endpoint)
	id := account(t, store, "peer", "refresh-0", time.Now().Add(-time.Minute), time.Now().Add(-time.Hour))
	peerWrites(t, store, endpoint, id, "refresh-peer", time.Now().Add(-time.Minute))

	if _, err := cred.NewAccountManager(store, mustSpec(t), id).Access(context.Background()); err == nil {
		t.Fatal("two refusals produced a token")
	}
	if got := endpoint.grants(); !slices.Equal(got, []string{"refresh-0", "refresh-peer"}) {
		t.Fatalf("grants %v, want exactly two", got)
	}
	if row := rowOf(t, store, id); row.DisabledCause == "" {
		t.Fatal("the peer's own token was refused and the account was not set aside")
	}
}

func TestEachRefusalNamesItsOwnReason(t *testing.T) {
	cases := []struct {
		name  string
		given refusal
		cause string
	}{
		{"expired", refusal{http.StatusUnauthorized, `{"error":{"code":"refresh_token_expired"}}`}, "expired"},
		{"reused", refusal{http.StatusUnauthorized, `{"error":{"code":"refresh_token_reused"}}`}, "already used"},
		{"invalidated", refusal{http.StatusUnauthorized, `{"error":{"code":"refresh_token_invalidated"}}`}, "revoked"},
		{"invalid grant", invalidGrant(), "invalid_grant"},
		{"bare 401", refusal{http.StatusUnauthorized, `unauthorized`}, ""},
		{"invalid token", refusal{http.StatusUnauthorized, `{"error":"invalid_token"}`}, ""},
		{"invalid grant on a 500", refusal{http.StatusInternalServerError, `{"error":"invalid_grant"}`}, ""},
	}
	var causes []string
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			endpoint := &grantLog{refuse: map[string]refusal{"refresh-0": c.given}}
			store := keptStore(t, endpoint)
			id := account(t, store, "refused", "refresh-0", time.Now().Add(-time.Minute), time.Now().Add(-time.Hour))
			if _, err := cred.NewAccountManager(store, mustSpec(t), id).Access(context.Background()); err == nil {
				t.Fatal("a refusal produced a token")
			}
			cause := rowOf(t, store, id).DisabledCause
			if c.cause == "" && cause != "" {
				t.Fatalf("a refusal that is not definitive set the account aside: %q", cause)
			}
			if !strings.Contains(cause, c.cause) || slices.Contains(causes, cause) && c.cause != "" {
				t.Fatalf("cause %q, want one naming %q and no other case's", cause, c.cause)
			}
			causes = append(causes, cause)
		})
	}
}

func TestAServerRefreshExpiryWarnsOnlyWhenNear(t *testing.T) {
	now := time.Now()
	far := cred.Row{Credential: cred.Credential{Provider: cred.ClaudeSub, RefreshExpires: now.Add(60 * 24 * time.Hour)}}
	near := cred.Row{Credential: cred.Credential{Provider: cred.ClaudeSub, RefreshExpires: now.Add(2 * 24 * time.Hour)}}
	gone := cred.Row{Credential: cred.Credential{Provider: cred.ClaudeSub, RefreshExpires: now.Add(-time.Hour)}}
	if _, dated := far.ReloginBy(now); dated {
		t.Fatal("a refresh token good for 60 days already warns")
	}
	if deadline, dated := near.ReloginBy(now); !dated || !deadline.Equal(near.Credential.RefreshExpires) {
		t.Fatalf("a refresh token good for 2 days gave %v %v", deadline, dated)
	}
	if gone.Unusable(now) == "" {
		t.Fatal("a refresh token the server said has expired reads usable")
	}
}

func mustSpec(t *testing.T) cred.Spec {
	t.Helper()
	spec, err := cred.Lookup(string(cred.ClaudeSub))
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	return spec
}
