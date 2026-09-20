package models

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
)

func windowsTheSideFileCarried() map[string]int {
	return map[string]int{
		"anthropic/claude-opus-5":              1000000,
		"anthropic/claude-opus-4-5-20251101":   200000,
		"anthropic/claude-sonnet-4-5-20250929": 200000,
		"anthropic/claude-haiku-4-5-20251001":  200000,
		"openai/gpt-5.5":                       400000,
	}
}

func TestEveryWindowTheSideFileResolvedStillResolvesThroughTheCatalog(t *testing.T) {
	catalog := shippedCatalog(t)
	for slug, want := range windowsTheSideFileCarried() {
		bySlug, found := catalog.ContextTokens(slug)
		if !found || bySlug != want {
			t.Fatalf("%s resolves to %d tokens, found %v, want the %d the side file carried", slug, bySlug, found, want)
		}
		_, bare, _ := strings.Cut(slug, "/")
		byBareName, found := catalog.ContextTokens(bare)
		if !found || byBareName != want {
			t.Fatalf("%s, the spelling every session header on disk carries, resolves to %d tokens, found %v, want %d", bare, byBareName, found, want)
		}
	}
}

func TestEveryDeclaredWindowUnderModelsResolvesThroughTheCatalog(t *testing.T) {
	catalog := shippedCatalog(t)
	files, err := filepath.Glob(filepath.Join(shippedRoot, modelsDir, "*", "*.yaml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("globbing the model files: %d found, %v", len(files), err)
	}
	declared := 0
	for _, path := range files {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		slug := filepath.Base(filepath.Dir(path)) + "/" + strings.TrimSuffix(filepath.Base(path), ".yaml")
		want := 0
		for _, line := range strings.Split(strings.ReplaceAll(string(body), "\r\n", "\n"), "\n") {
			field, value, cut := strings.Cut(line, ":")
			if !cut || strings.TrimSpace(field) != "context_tokens" {
				continue
			}
			if want, err = strconv.Atoi(strings.TrimSpace(value)); err != nil {
				t.Fatalf("%s declares %q, which the loader would have refused", slug, value)
			}
		}
		got, found := catalog.ContextTokens(slug)
		if want == 0 {
			if found {
				t.Fatalf("%s declares no window and the catalog answered %d, so a model nobody has a number for would compact on a guess", slug, got)
			}
			continue
		}
		declared++
		if !found || got != want {
			t.Fatalf("%s declares %d tokens in its own file and the catalog answered %d, found %v", slug, want, got, found)
		}
	}
	if declared != len(windowsTheSideFileCarried()) {
		t.Fatalf("%d model files declare a window, want the %d carried over from the side file", declared, len(windowsTheSideFileCarried()))
	}
}

func TestAMalformedWindowIsRefusedByFileAndByFieldName(t *testing.T) {
	for _, declared := range []string{"200k", "0", "-1", "two hundred thousand"} {
		catalog, err := Load([]Layer{layerOf("catalog", withSubscriptions(oneFile(
			"models/openai/gpt-5.6-sol.yaml", "subscription: codex\nuse: default\ncontext_tokens: "+declared+"\n")))})
		if err == nil {
			t.Fatalf("context_tokens: %s loaded, and a window nobody can read is worse than an absent one", declared)
		}
		if len(catalog.Broken) != 1 {
			t.Fatalf("context_tokens: %s produced %d refusals, want the one file that carries it", declared, len(catalog.Broken))
		}
		refused := catalog.Broken[0]
		if refused.Field != "context_tokens" || !strings.HasSuffix(refused.File, "gpt-5.6-sol.yaml") {
			t.Fatalf("context_tokens: %s is refused as %q, and tofu catalog prints the file and the field", declared, refused.Error())
		}
		t.Log(refused.Error())
	}
}

func TestAModelWithNoDeclaredWindowResolvesToNoWindowAtAll(t *testing.T) {
	catalog := shippedCatalog(t)
	for _, slug := range []string{"anthropic/claude-sonnet-5", "anthropic/claude-opus-4-6", "openai/gpt-5.6-sol"} {
		if tokens, found := catalog.ContextTokens(slug); found {
			t.Fatalf("%s has no window in its file and the catalog answered %d tokens", slug, tokens)
		}
	}
}

func TestABareNameTwoProvidersServeResolvesToNoWindow(t *testing.T) {
	catalog, err := Load([]Layer{layerOf("catalog", withSubscriptions(fstest.MapFS{
		"models/openai/twin.yaml":    &fstest.MapFile{Data: []byte("subscription: codex\nuse: default\ncontext_tokens: 400000\n")},
		"models/anthropic/twin.yaml": &fstest.MapFile{Data: []byte("subscription: claude\nuse: default\ncontext_tokens: 200000\n")},
	}))})
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	if tokens, found := catalog.ContextTokens("twin"); found {
		t.Fatalf("a bare name two vendors both serve resolved to %d tokens, and picking one of two is a guess", tokens)
	}
	if tokens, found := catalog.ContextTokens("openai/twin"); !found || tokens != 400000 {
		t.Fatalf("the same model by its identity resolved to %d tokens, found %v, want 400000", tokens, found)
	}
}

func TestAProjectLayerOverridesTheWindowAndNothingElse(t *testing.T) {
	catalog, err := Load([]Layer{
		layerOf("catalog", withSubscriptions(oneFile(
			"models/anthropic/claude-opus-5.yaml", "subscription: claude\nuse: default\ncontext_tokens: 1000000\n"))),
		layerOf("project", oneFile("models/anthropic/claude-opus-5.yaml", "context_tokens: 500000\n")),
	})
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	tokens, found := catalog.ContextTokens("anthropic/claude-opus-5")
	if !found || tokens != 500000 {
		t.Fatalf("the project layer set a 500000 token window and the catalog answered %d, found %v", tokens, found)
	}
	model, err := catalog.Select("anthropic/claude-opus-5")
	if err != nil {
		t.Fatalf("the project layer took the model with it: %v", err)
	}
	if model.Use != UseDefault || model.Subscription != Claude {
		t.Fatalf("a project file naming one field dropped the rest: %+v", model)
	}
}
