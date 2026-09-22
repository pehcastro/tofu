package models

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"tofu/internal/session"
)

func TestEveryShippedModelPaidBySubscriptionCarriesTheSuffix(t *testing.T) {
	library := shippedLibrary(t)
	if len(library.Models) == 0 {
		t.Fatal("the shipped library loaded no models at all")
	}
	for _, model := range library.Models {
		if model.Subscription == "" {
			t.Fatalf("%s carries no subscription, and every shipped model is paid by one", model.VendorSlug())
		}
		want := string(model.Subscription) + "-sub/" + model.ID
		if model.Slug() != want {
			t.Fatalf("%s reads %q, want %q", model.VendorSlug(), model.Slug(), want)
		}
	}
}

func TestAModelWithNoSubscriptionFieldReadsTheBareVendor(t *testing.T) {
	library, err := Load([]Layer{layerOf("library", withSubscriptions(oneFile(
		"models/anthropic/claude-direct.yaml", "use: allowed\n")))})
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	model, err := library.Select("anthropic/claude-direct")
	if err != nil {
		t.Fatalf("a direct-key model does not resolve by vendor/name: %v", err)
	}
	if model.Subscription != "" {
		t.Fatalf("a model with no subscription field carries one anyway: %q", model.Subscription)
	}
}

func TestTheSubSuffixIsDerivedNeverWrittenInTheSubscriptionFile(t *testing.T) {
	library, err := Load([]Layer{layerOf("library", fstest.MapFS{
		"subscriptions/opencode.yaml":       &fstest.MapFile{Data: []byte("provider: anthropic\nwire: opencode\nwindows: 5h\n")},
		"models/anthropic/deepseek-v3.yaml": &fstest.MapFile{Data: []byte("subscription: opencode\nuse: default\n")},
	})})
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	model, err := library.Select("opencode-sub/deepseek-v3")
	if err != nil {
		t.Fatalf("a new subscription's model does not resolve to the derived suffix: %v", err)
	}
	if model.Subscription != "opencode" {
		t.Fatalf("the model carries subscription %q", model.Subscription)
	}
}

func TestABareModelNameFromARecordedSessionStillResolves(t *testing.T) {
	library := shippedLibrary(t)
	model, found := library.Resolve("claude-opus-5")
	if !found || model.Slug() != "claude-sub/claude-opus-5" {
		t.Fatalf("a bare recorded model name resolved to %+v, found %v", model, found)
	}
}

func TestARealRecordedSessionReadsBackAndResolvesItsModel(t *testing.T) {
	source := filepath.Join("..", "..", "..", ".tofu", "sessions", "turn-18d6d2c2a3eb1900.json")
	raw, err := os.ReadFile(source)
	if err != nil {
		t.Skipf("no recorded session to prove this against: %v", err)
	}
	dir := t.TempDir()
	sessionsDir := filepath.Join(dir, "sessions")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("making the sessions directory: %v", err)
	}
	copyPath := filepath.Join(sessionsDir, "turn-18d6d2c2a3eb1900.json")
	if err := os.WriteFile(copyPath, raw, 0o644); err != nil {
		t.Fatalf("writing the copy: %v", err)
	}
	store := session.OpenAt(dir)
	header, err := store.Header("turn-18d6d2c2a3eb1900")
	if err != nil {
		t.Fatalf("reading the copy back: %v", err)
	}
	library := shippedLibrary(t)
	model, found := library.Resolve(header.Model)
	if !found {
		t.Fatalf("the session names model %q and the library cannot resolve it", header.Model)
	}
	if model.Slug() != "claude-sub/claude-opus-5" {
		t.Fatalf("the recorded session resolved to %q, want claude-sub/claude-opus-5", model.Slug())
	}
}
