package models

import (
	"os"
	"path/filepath"
	"strings"
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
		if !strings.HasSuffix(string(model.Subscription), "-sub") {
			t.Fatalf("%s is paid by %q, and a subscription says so in its own name", model.VendorSlug(), model.Subscription)
		}
		want := string(model.Subscription) + "/" + model.ID
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

func TestASubscriptionIsSpelledTheSameWayInItsFileNameAndInEverySlug(t *testing.T) {
	library, err := Load([]Layer{layerOf("library", fstest.MapFS{
		"subscriptions/opencode-sub.yaml":   &fstest.MapFile{Data: []byte("provider: anthropic\nwire: opencode\nwindows: 5h\n")},
		"models/anthropic/deepseek-v3.yaml": &fstest.MapFile{Data: []byte("subscription: opencode-sub\nuse: default\n")},
	})})
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	model, err := library.Select("opencode-sub/deepseek-v3")
	if err != nil {
		t.Fatalf("a new subscription's model does not resolve by the name its file carries: %v", err)
	}
	if model.Subscription != "opencode-sub" {
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

func realRecordedHeader(t *testing.T, id string) session.Header {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", ".tofu", "sessions", id+".json"))
	if err != nil {
		t.Skipf("no recorded session %s to prove this against: %v", id, err)
	}
	dir := t.TempDir()
	sessionsDir := filepath.Join(dir, "sessions")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("making the sessions directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessionsDir, id+".json"), raw, 0o644); err != nil {
		t.Fatalf("writing the copy: %v", err)
	}
	header, err := session.OpenAt(dir).Header(id)
	if err != nil {
		t.Fatalf("reading the copy of %s back: %v", id, err)
	}
	return header
}

func TestARealRecordedSessionReadsBackAndResolvesItsModel(t *testing.T) {
	header := realRecordedHeader(t, "turn-18d6d2c2a3eb1900")
	library := shippedLibrary(t)
	model, found := library.Resolve(header.Model)
	if !found {
		t.Fatalf("the session names model %q and the library cannot resolve it", header.Model)
	}
	if model.Slug() != "claude-sub/claude-opus-5" {
		t.Fatalf("the recorded session resolved to %q, want claude-sub/claude-opus-5", model.Slug())
	}
}

func TestARealSessionRecordedUnderAVendorSlugReadsBackAndIsNotTakenForASubscription(t *testing.T) {
	header := realRecordedHeader(t, "turn-18d68bcceb3d56e8")
	if header.Model == "" {
		t.Fatal("the recorded session names no model, so nothing here is proved")
	}
	vendor, _, cut := strings.Cut(header.Model, "/")
	if !cut || !Provider(vendor).valid() {
		t.Fatalf("this session was meant to carry a vendor slug and carries %q", header.Model)
	}
	if model, found := shippedLibrary(t).Resolve(header.Model); found {
		t.Fatalf("a row paid by a key resolved to %q, which a subscription pays for", model.Slug())
	}
}
