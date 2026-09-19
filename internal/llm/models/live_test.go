package models

import (
	"context"
	"os"
	"testing"
	"time"

	"boji/internal/llm/cred"
	"boji/internal/transport"
)

func liveAccount(t *testing.T, provider Provider) Account {
	t.Helper()
	if os.Getenv("BOJI_LIVE_MODELS") != "1" {
		t.Skip("set BOJI_LIVE_MODELS=1 to ask the live subscription which models it serves")
	}
	path, err := cred.Path()
	if err != nil {
		t.Fatalf("locating the credential store: %v", err)
	}
	store, err := cred.Open(path)
	if err != nil {
		t.Skipf("no credential store, run boji login %s: %v", provider, err)
	}
	t.Cleanup(func() { _ = store.Close() })
	spec, err := cred.Lookup(string(provider))
	if err != nil {
		t.Fatalf("looking up the %s spec: %v", provider, err)
	}
	row, present, err := store.Row(cred.Provider(provider))
	if err != nil || !present {
		t.Skipf("no %s credential: %v", provider, err)
	}
	return Account{
		Provider:  provider,
		AccountID: row.Credential.Identity.AccountID,
		Token:     cred.NewManager(store, spec).Access,
	}
}

func TestLiveDiscovery(t *testing.T) {
	client, err := transport.New(transport.Config{AttemptTimeout: 30 * time.Second, Concurrency: 1})
	if err != nil {
		t.Fatalf("building the transport: %v", err)
	}
	for _, provider := range Providers() {
		t.Run(string(provider), func(t *testing.T) {
			account := liveAccount(t, provider)
			served, err := Discover(context.Background(), client, account)
			if err != nil {
				t.Fatalf("discovery against %s under %s: %v", provider, served.Pin, err)
			}
			catalog, err := Load(os.DirFS("../../../catalog/models"))
			if err != nil {
				t.Fatalf("loading the catalog: %v", err)
			}
			for _, line := range catalog.Reconcile(served).Lines() {
				t.Log(line)
			}
		})
	}
}
