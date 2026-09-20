package models

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

const shippedRoot = "../../../catalog"

func shippedLayer() Layer {
	return Layer{Name: "catalog", Origin: "catalog", FS: os.DirFS(shippedRoot)}
}

func shippedCatalog(t *testing.T) Catalog {
	t.Helper()
	catalog, err := Load([]Layer{shippedLayer()})
	if err != nil {
		t.Fatalf("loading the shipped catalog: %v", err)
	}
	return catalog
}

func oneFile(name, body string) fstest.MapFS {
	return fstest.MapFS{name: &fstest.MapFile{Data: []byte(body)}}
}

func withSubscriptions(extra fstest.MapFS) fstest.MapFS {
	base := fstest.MapFS{
		"subscriptions/claude.yaml": &fstest.MapFile{Data: []byte("provider: anthropic\nwire: anthropic\nwindows: 5h, 7d\n")},
		"subscriptions/codex.yaml":  &fstest.MapFile{Data: []byte("provider: openai\nwire: codex\nwindows: 5h, 7d\n")},
	}
	for name, file := range extra {
		base[name] = file
	}
	return base
}

func layerOf(name string, files fstest.MapFS) Layer {
	return Layer{Name: name, Origin: name, FS: files}
}

func TestEveryShippedModelResolvesByItsSlugAndByThatAlone(t *testing.T) {
	catalog := shippedCatalog(t)
	if len(catalog.Models) == 0 {
		t.Fatal("the shipped catalog loaded no models at all")
	}
	for _, model := range catalog.Models {
		found, err := catalog.Select(model.Slug())
		switch {
		case model.Use == UseExcluded:
			var refusal *Refusal
			if !errors.As(err, &refusal) || refusal.Slug != model.Slug() {
				t.Fatalf("%s is excluded and the refusal does not name it: %v", model.Slug(), err)
			}
		case err != nil:
			t.Fatalf("%s does not resolve by its slug: %v", model.Slug(), err)
		case found.File != model.File:
			t.Fatalf("%s resolved to %s", model.Slug(), found.File)
		}
		if _, err := catalog.Select(model.ID); err == nil {
			t.Fatalf("%q resolved without its provider, so the slug is not the identity", model.ID)
		}
	}
}

func TestEveryFileUnderModelsIsAModel(t *testing.T) {
	root := filepath.Join(shippedRoot, modelsDir)
	providers, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("reading %s: %v", root, err)
	}
	catalog := shippedCatalog(t)
	filed := map[string]bool{}
	for _, model := range catalog.Models {
		filed[model.Slug()+".yaml"] = true
	}
	for _, provider := range providers {
		if !provider.IsDir() {
			t.Fatalf("%s is not a provider directory, so it is not a model", provider.Name())
		}
		if !Provider(provider.Name()).valid() {
			t.Fatalf("%s is not a vendor tofu reaches", provider.Name())
		}
		entries, err := os.ReadDir(filepath.Join(root, provider.Name()))
		if err != nil {
			t.Fatalf("reading %s: %v", provider.Name(), err)
		}
		for _, entry := range entries {
			if !filed[provider.Name()+"/"+entry.Name()] {
				t.Fatalf("%s/%s is under catalog/models and is not a model", provider.Name(), entry.Name())
			}
		}
	}
}

func TestLoadRefusesAFileUnderModelsThatIsNotAModel(t *testing.T) {
	_, err := Load([]Layer{layerOf("catalog", withSubscriptions(fstest.MapFS{
		"models/README.md":               &fstest.MapFile{Data: []byte("notes\n")},
		"models/openai/gpt-5.6-sol.yaml": &fstest.MapFile{Data: []byte("subscription: codex\nuse: default\n")},
	}))})
	if err == nil || !strings.Contains(err.Error(), "models/README.md") {
		t.Fatalf("want the stray file refused by name, got %v", err)
	}
}

func TestLoadRefusesAModelMissingARequiredFieldAndNamesTheField(t *testing.T) {
	_, err := Load([]Layer{layerOf("catalog", withSubscriptions(oneFile(
		"models/openai/gpt-5.6-sol.yaml", "subscription: codex\nuse: excluded\n")))})
	var refused *BrokenCatalog
	if !errors.As(err, &refused) || len(refused.Refused) == 0 {
		t.Fatalf("want a refusal, got %v", err)
	}
	first := refused.Refused[0]
	if !strings.Contains(first.File, "gpt-5.6-sol.yaml") || first.Field != "reason" {
		t.Fatalf("the refusal names %q and field %q, want the file and reason", first.File, first.Field)
	}
}

func TestLoadRefusesAModelFiledUnderTheWrongVendor(t *testing.T) {
	_, err := Load([]Layer{layerOf("catalog", withSubscriptions(oneFile(
		"models/anthropic/gpt-5.6-sol.yaml", "subscription: codex\nuse: default\n")))})
	if err == nil || !strings.Contains(err.Error(), "wrong vendor") {
		t.Fatalf("want the vendor mismatch refused, got %v", err)
	}
}

func TestLoadRefusesAnUnknownField(t *testing.T) {
	_, err := Load([]Layer{layerOf("catalog", withSubscriptions(oneFile(
		"models/openai/gpt-5.6-sol.yaml", "subscription: codex\nuse: default\nprice: 3\n")))})
	if err == nil || !strings.Contains(err.Error(), `price: unknown field`) {
		t.Fatalf("want the unknown field refused by name, got %v", err)
	}
}

func TestLoadRefusesTwoDefaultsForOneSubscription(t *testing.T) {
	_, err := Load([]Layer{layerOf("catalog", withSubscriptions(fstest.MapFS{
		"models/openai/gpt-5.6-sol.yaml":  &fstest.MapFile{Data: []byte("subscription: codex\nuse: default\n")},
		"models/openai/gpt-5.6-luna.yaml": &fstest.MapFile{Data: []byte("subscription: codex\nuse: default\n")},
	}))})
	if err == nil || !strings.Contains(err.Error(), "already has a default") {
		t.Fatalf("want the second default refused, got %v", err)
	}
}

func TestLoadDefaultsAnUndeclaredUseToExcluded(t *testing.T) {
	catalog, err := Load([]Layer{layerOf("catalog", withSubscriptions(fstest.MapFS{
		"models/openai/gpt-5.6-sol.yaml":  &fstest.MapFile{Data: []byte("subscription: codex\nuse: default\n")},
		"models/openai/gpt-5.6-luna.yaml": &fstest.MapFile{Data: []byte("subscription: codex\n")},
	}))})
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	quiet, err := catalog.Select("openai/gpt-5.6-luna")
	if err == nil {
		t.Fatalf("a model with no use must not be sendable, got %+v", quiet)
	}
	if !strings.Contains(err.Error(), "until somebody says it may") {
		t.Fatalf("the refusal must say why, got %v", err)
	}
}

func TestProjectOverridesGlobalFieldByField(t *testing.T) {
	global := layerOf("global", withSubscriptions(oneFile(
		"models/openai/gpt-5.6-sol.yaml", "subscription: codex\nwindow: 7d:sol\nuse: excluded\nreason: the global file says no\n")))
	project := layerOf("project", oneFile(
		"models/openai/gpt-5.6-sol.yaml", "use: default\n"))
	catalog, err := Load([]Layer{global, project})
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	model, err := catalog.Select("openai/gpt-5.6-sol")
	if err != nil {
		t.Fatalf("the project layer did not win: %v", err)
	}
	if model.Use != UseDefault {
		t.Fatalf("use is %q, want the project value", model.Use)
	}
	if model.Reason != "the global file says no" || model.WindowText() != "5h and 7d and 7d:sol" {
		t.Fatalf("the project file replaced the whole entry rather than one field: %+v", model)
	}
	if !strings.HasPrefix(model.File, "project") {
		t.Fatalf("the entry does not report the file that last wrote it: %q", model.File)
	}
}

func TestTheShippedWiresResolveTheModelsTheyResolvedBefore(t *testing.T) {
	catalog := shippedCatalog(t)
	for wire, want := range map[string]string{"anthropic": "claude-opus-5", "codex": "gpt-5.6-sol"} {
		spec, carried := catalog.ForWire(wire)
		if !carried {
			t.Fatalf("no subscription answers to --wire %s", wire)
		}
		model, err := catalog.Default(spec.ID)
		if err != nil {
			t.Fatalf("--wire %s has no default: %v", wire, err)
		}
		if model.ID != want {
			t.Fatalf("--wire %s now sends %q, it sent %q", wire, model.ID, want)
		}
	}
	frozen := map[string]string{
		"anthropic/claude-fable-5":             "claude-fable-5 excluded 5h and 7d and 7d:fable",
		"anthropic/claude-fable-5-1":           "claude-fable-5-1 excluded 5h and 7d and 7d:fable",
		"anthropic/claude-haiku-4-5-20251001":  "claude-haiku-4-5-20251001 allowed 5h and 7d",
		"anthropic/claude-opus-4-5-20251101":   "claude-opus-4-5-20251101 excluded 5h and 7d",
		"anthropic/claude-opus-4-6":            "claude-opus-4-6 excluded 5h and 7d",
		"anthropic/claude-opus-4-7":            "claude-opus-4-7 excluded 5h and 7d",
		"anthropic/claude-opus-4-8":            "claude-opus-4-8 excluded 5h and 7d",
		"anthropic/claude-opus-5":              "claude-opus-5 default 5h and 7d",
		"anthropic/claude-sonnet-4-5-20250929": "claude-sonnet-4-5-20250929 excluded 5h and 7d",
		"anthropic/claude-sonnet-4-6":          "claude-sonnet-4-6 excluded 5h and 7d",
		"anthropic/claude-sonnet-5":            "claude-sonnet-5 allowed 5h and 7d",
		"openai/gpt-5.5":                       "gpt-5.5 excluded 5h and 7d",
		"openai/gpt-5.6-luna":                  "gpt-5.6-luna allowed 5h and 7d",
		"openai/gpt-5.6-sol":                   "gpt-5.6-sol default 5h and 7d",
		"openai/gpt-5.6-terra":                 "gpt-5.6-terra allowed 5h and 7d",
		"openai/gpt-6-astra":                   "gpt-6-astra excluded 5h and 7d",
		"openai/gpt-reserve":                   "gpt-reserve excluded 5h and 7d",
	}
	if len(catalog.Models) != len(frozen) {
		t.Fatalf("the catalog carries %d models and the frozen table has %d", len(catalog.Models), len(frozen))
	}
	for _, model := range catalog.Models {
		got := model.ID + " " + string(model.Use) + " " + model.WindowText()
		if want := frozen[model.Slug()]; got != want {
			t.Fatalf("%s now reads %q, it read %q", model.Slug(), got, want)
		}
	}
}

func TestCodexAutoReviewIsAServedNameRatherThanAModel(t *testing.T) {
	catalog := shippedCatalog(t)
	if _, err := catalog.Select("openai/codex-auto-review"); err == nil {
		t.Fatal("codex-auto-review is still a model")
	}
	reconciled := catalog.Reconcile(Served{Subscription: Codex, Pin: "test", IDs: []string{"gpt-5.6-sol", "codex-auto-review"}})
	if len(reconciled.Unknown) != 0 {
		t.Fatalf("the account serves %v and the catalog cannot account for them", reconciled.Unknown)
	}
}

func TestSelectRefusesAnUnknownModelAndNamesWhatItKnows(t *testing.T) {
	_, err := shippedCatalog(t).Select("anthropic/claude-opus-latest")
	var refusal *Refusal
	if !errors.As(err, &refusal) || refusal.Kind != RefusedUnknown {
		t.Fatalf("want an unknown refusal, got %v", err)
	}
	if !strings.Contains(refusal.Error(), "anthropic/claude-opus-5") {
		t.Fatalf("the refusal must name what the catalog knows by slug, got %q", refusal.Error())
	}
}

func TestSelectRefusesAnExcludedModelWithItsCatalogReason(t *testing.T) {
	_, err := shippedCatalog(t).Select("anthropic/claude-fable-5-1")
	var refusal *Refusal
	if !errors.As(err, &refusal) || refusal.Kind != RefusedExcluded {
		t.Fatalf("want an exclusion refusal, got %v", err)
	}
	if !strings.Contains(refusal.Error(), "not fable or astra for now") {
		t.Fatalf("the refusal must carry the catalog reason, got %q", refusal.Error())
	}
}

func TestSubscriptionsCarryTheQuotaWindowsAndTheModelsDoNot(t *testing.T) {
	catalog := shippedCatalog(t)
	if len(catalog.Subscriptions) != 2 {
		t.Fatalf("want the claude and codex subscriptions, got %+v", catalog.Subscriptions)
	}
	for _, spec := range catalog.Subscriptions {
		if len(spec.Windows) == 0 || spec.Wire == "" || !spec.Provider.valid() {
			t.Fatalf("%s is missing a fact: %+v", spec.ID, spec)
		}
	}
	body, err := os.ReadFile(filepath.Join(shippedRoot, modelsDir, "anthropic", "claude-opus-5.yaml"))
	if err != nil {
		t.Fatalf("reading the default model file: %v", err)
	}
	if strings.Contains(string(body), "provider:") || strings.Contains(string(body), "windows:") {
		t.Fatalf("a model file still carries a vendor or a quota window:\n%s", body)
	}
}
