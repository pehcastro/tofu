package models

import (
	"context"
	"os"
	"testing"
	"time"

	"tofu/internal/llm/cred"
	"tofu/internal/transport"
)

func liveAccount(t *testing.T, spec SubscriptionSpec) Account {
	t.Helper()
	if os.Getenv("TOFU_LIVE_MODELS") != "1" {
		t.Skip("set TOFU_LIVE_MODELS=1 to ask the live subscription which models it serves")
	}
	path, err := cred.Path()
	if err != nil {
		t.Fatalf("locating the credential store: %v", err)
	}
	store, err := cred.Open(path)
	if err != nil {
		t.Skipf("no credential store, run tofu login %s: %v", spec.Wire, err)
	}
	t.Cleanup(func() { _ = store.Close() })
	credential, err := cred.Lookup(spec.Wire)
	if err != nil {
		t.Fatalf("looking up the %s spec: %v", spec.Wire, err)
	}
	row, present, err := store.Row(cred.Provider(spec.Wire))
	if err != nil || !present {
		t.Skipf("no %s credential: %v", spec.Wire, err)
	}
	return Account{
		Subscription: spec.ID,
		AccountID:    row.Credential.Identity.AccountID,
		Token:        cred.NewManager(store, credential).Access,
	}
}

func TestLiveDiscovery(t *testing.T) {
	client, err := transport.New(transport.Config{AttemptTimeout: 30 * time.Second, Concurrency: 1})
	if err != nil {
		t.Fatalf("building the transport: %v", err)
	}
	catalog := shippedCatalog(t)
	for _, spec := range catalog.Subscriptions {
		t.Run(string(spec.ID), func(t *testing.T) {
			served, err := Discover(context.Background(), client, liveAccount(t, spec))
			if err != nil {
				t.Fatalf("discovery against %s under %s: %v", spec.ID, served.Pin, err)
			}
			for _, line := range catalog.Reconcile(served).Lines() {
				t.Log(line)
			}
		})
	}
}
