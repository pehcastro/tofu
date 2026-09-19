package models

import (
	"errors"
	"os"
	"strings"
	"testing"
	"testing/fstest"
)

func shippedCatalog(t *testing.T) Catalog {
	t.Helper()
	catalog, err := Load(os.DirFS("../../../catalog/models"))
	if err != nil {
		t.Fatalf("loading the shipped catalog: %v", err)
	}
	return catalog
}

func TestLoadRefusesAnExcludedEntryWithNoReason(t *testing.T) {
	_, err := Load(fstest.MapFS{
		"one.yaml": &fstest.MapFile{Data: []byte("id: one\nprovider: codex\nwindow: 7d\nuse: excluded\n")},
		"two.yaml": &fstest.MapFile{Data: []byte("id: two\nprovider: codex\nwindow: 7d\nuse: default\n")},
	})
	if err == nil || !strings.Contains(err.Error(), "the reason it was excluded") {
		t.Fatalf("want a refusal naming the missing reason, got %v", err)
	}
}

func TestLoadDefaultsAnUndeclaredUseToExcluded(t *testing.T) {
	catalog, err := Load(fstest.MapFS{
		"one.yaml": &fstest.MapFile{Data: []byte("id: one\nprovider: codex\nwindow: 7d\n")},
		"two.yaml": &fstest.MapFile{Data: []byte("id: two\nprovider: codex\nwindow: 7d\nuse: default\n")},
	})
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	if catalog.Models[0].Use != UseExcluded || catalog.Models[0].Reason == "" {
		t.Fatalf("an entry with no use must load excluded and carry a reason, got %+v", catalog.Models[0])
	}
}

func TestLoadRefusesTwoDefaultsForOneProvider(t *testing.T) {
	_, err := Load(fstest.MapFS{
		"one.yaml": &fstest.MapFile{Data: []byte("id: one\nprovider: codex\nwindow: 7d\nuse: default\n")},
		"two.yaml": &fstest.MapFile{Data: []byte("id: two\nprovider: codex\nwindow: 7d\nuse: default\n")},
	})
	if err == nil || !strings.Contains(err.Error(), "already has a default") {
		t.Fatalf("want a refusal naming the second default, got %v", err)
	}
}

func TestLoadRefusesAnUnknownField(t *testing.T) {
	_, err := Load(fstest.MapFS{"one.yaml": &fstest.MapFile{
		Data: []byte("id: one\nprovider: codex\nwindow: 7d\nuse: default\nprice: 3\n"),
	}})
	if err == nil || !strings.Contains(err.Error(), `unknown field "price"`) {
		t.Fatalf("want a refusal naming the unknown field, got %v", err)
	}
}

func TestSelectRefusesAnExcludedModelWithItsCatalogReason(t *testing.T) {
	_, err := shippedCatalog(t).Select(Anthropic, "claude-fable-5-1")
	var refusal *Refusal
	if !errors.As(err, &refusal) || refusal.Kind != RefusedExcluded {
		t.Fatalf("want an exclusion refusal, got %v", err)
	}
	if !strings.Contains(refusal.Error(), "not fable or astra for now") {
		t.Fatalf("the refusal must carry the catalog reason, got %q", refusal.Error())
	}
}

func TestSelectRefusesAnUnknownModelAndNamesWhatItKnows(t *testing.T) {
	_, err := shippedCatalog(t).Select(Anthropic, "claude-opus-latest")
	var refusal *Refusal
	if !errors.As(err, &refusal) || refusal.Kind != RefusedUnknown {
		t.Fatalf("want an unknown refusal, got %v", err)
	}
	if !strings.Contains(refusal.Error(), "claude-opus-5") {
		t.Fatalf("the refusal must name what the catalog knows, got %q", refusal.Error())
	}
}

func TestShippedDefaultsAreTheOwnersRuling(t *testing.T) {
	catalog := shippedCatalog(t)
	for provider, want := range map[Provider]string{Anthropic: "claude-opus-5", Codex: "gpt-5.6-sol"} {
		model, err := catalog.Default(provider)
		if err != nil {
			t.Fatalf("default for %s: %v", provider, err)
		}
		if model.ID != want {
			t.Fatalf("default for %s is %q, want %q", provider, model.ID, want)
		}
		if len(model.Windows) == 0 {
			t.Fatalf("%s names no window", model.ID)
		}
	}
}
