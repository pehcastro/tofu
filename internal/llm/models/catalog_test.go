package models

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"tofu/internal/llm"
	"tofu/internal/sys"
)

const foundToday = "2026-09-28"

func registryOf(t *testing.T, body string) Registry {
	t.Helper()
	registry, err := ParseRegistry([]byte(body), "models.dev under test")
	if err != nil {
		t.Fatalf("parsing the registry under test: %v", err)
	}
	return registry
}

func planOf(library Library, subscription Subscription, registry Registry, ids ...string) CatalogPlan {
	served := Served{Subscription: subscription, Pin: "the pin under test", IDs: ids}
	return Plan(library.Reconcile(served, registry), registry, library, foundToday)
}

func entryIn(t *testing.T, plan CatalogPlan, slug string) Model {
	t.Helper()
	for _, model := range plan {
		if model.Slug() == slug {
			return model
		}
	}
	t.Fatalf("the plan has no %s, it has %v", slug, plan)
	return Model{}
}

func namesAFactOnly(t *testing.T, model Model) {
	t.Helper()
	for _, banned := range []string{"owner", "2026", "preference"} {
		if strings.Contains(model.Reason, banned) {
			t.Fatalf("%s is excluded for %q, which names %q rather than a fact", model.Slug(), model.Reason, banned)
		}
	}
}

func TestANewIdLandsAllowedWithTheEffortsOfItsNewestAllowedSibling(t *testing.T) {
	library := shippedLibrary(t)
	sibling, _ := library.Resolve("claude-sub/claude-sonnet-5")
	registry := registryOf(t, `{"anthropic":{"models":{"claude-sonnet-5-5":{"tool_call":true,"reasoning":true,`+
		`"reasoning_options":[{"type":"effort","values":["high"]}],"modalities":{"input":["text"]},"limit":{"context":1000000}}}}}`)
	plan := planOf(library, ClaudeSub, registry, "claude-sonnet-5-5", "claude-sonnet-5-5-20261001")
	if len(plan) != 2 {
		t.Fatalf("want the two new ids and nothing else, got %v", plan)
	}
	for _, slug := range []string{"claude-sub/claude-sonnet-5-5", "claude-sub/claude-sonnet-5-5-20261001"} {
		entry := entryIn(t, plan, slug)
		if entry.Use != UseAllowed || entry.Reason != "" {
			t.Fatalf("%s is %q %q, want allowed", slug, entry.Use, entry.Reason)
		}
		if !slices.Equal(entry.Efforts, sibling.Efforts) || entry.Vision != sibling.Vision {
			t.Fatalf("%s took efforts %v vision %q, want claude-sonnet-5's %v %q", slug, entry.Efforts, entry.Vision, sibling.Efforts, sibling.Vision)
		}
		if !strings.Contains(entry.From, "claude-sub/claude-sonnet-5") || entry.Found != foundToday {
			t.Fatalf("%s does not say where it came from: from %q found %q", slug, entry.From, entry.Found)
		}
	}
}

func TestANewIdInAFamilyTheLibraryExcludesLandsAllowed(t *testing.T) {
	library, registry := shippedLibrary(t), shippedTable(t)
	fable := entryIn(t, planOf(library, ClaudeSub, registry, "claude-fable-5-2"), "claude-sub/claude-fable-5-2")
	astra := entryIn(t, planOf(library, CodexSub, registry, "gpt-6.1-astra"), "codex-sub/gpt-6.1-astra")
	for _, entry := range []Model{fable, astra} {
		if entry.Use != UseAllowed {
			t.Fatalf("%s landed %q %q, and no fact excludes it", entry.Slug(), entry.Use, entry.Reason)
		}
	}
}

func TestOnlyAMissingToolCallExcludesANewIdAndAnUnlistedOneIsNotMissing(t *testing.T) {
	library := shippedLibrary(t)
	registry := registryOf(t, `{"openai":{"models":{"gpt-5.7-mini":{"tool_call":false,"limit":{"context":400000}}}}}`)
	plan := planOf(library, CodexSub, registry, "gpt-5.7-mini", "gpt-5.7-sol")
	mini := entryIn(t, plan, "codex-sub/gpt-5.7-mini")
	if mini.Use != UseExcluded || !strings.Contains(mini.Reason, "tool calls") {
		t.Fatalf("an id models.dev lists without tool calls landed %q %q", mini.Use, mini.Reason)
	}
	namesAFactOnly(t, mini)
	sol := entryIn(t, plan, "codex-sub/gpt-5.7-sol")
	solSibling, _ := library.Resolve("codex-sub/gpt-5.6-sol")
	if sol.Use != UseAllowed || !slices.Equal(sol.Efforts, solSibling.Efforts) {
		t.Fatalf("an id models.dev does not list landed %q with %v, want allowed with gpt-5.6-sol's efforts", sol.Use, sol.Efforts)
	}
}

func TestACatalogEntryNoLongerServedIsExcludedAndComesBackWhenServedAgain(t *testing.T) {
	catalog := layerOf(catalogLayer, oneFile("models/anthropic/claude-sonnet-5-5.yaml",
		"subscription: claude-sub\nuse: allowed\nfrom: the claude-sub account\nfound: 2026-09-01\n"))
	library, err := Load([]Layer{shippedLayer(), catalog})
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	plan := planOf(library, ClaudeSub, shippedTable(t), "claude-opus-5")
	if len(plan) != 1 {
		t.Fatalf("want the one catalog entry and no shipped one, got %v", plan)
	}
	gone := entryIn(t, plan, "claude-sub/claude-sonnet-5-5")
	if gone.Use != UseExcluded || !strings.Contains(gone.Reason, "claude-sub") || gone.Found != "2026-09-01" {
		t.Fatalf("a catalog entry the account stopped serving came out %q %q found %q", gone.Use, gone.Reason, gone.Found)
	}
	namesAFactOnly(t, gone)

	dir := t.TempDir()
	if err := plan.Write(dir); err != nil {
		t.Fatalf("writing: %v", err)
	}
	library, err = Load([]Layer{shippedLayer(), sys.DirLayer(catalogLayer, dir)})
	if err != nil {
		t.Fatalf("loading the written catalog: %v", err)
	}
	back := entryIn(t, planOf(library, ClaudeSub, shippedTable(t), "claude-opus-5", "claude-sonnet-5-5"), "claude-sub/claude-sonnet-5-5")
	if back.Use != UseAllowed || back.Found != "2026-09-01" {
		t.Fatalf("served again, the entry came out %q %q found %q", back.Use, back.Reason, back.Found)
	}
}

func TestAGlobalFileOverridesTheCatalogFieldByField(t *testing.T) {
	catalog := layerOf(catalogLayer, oneFile("models/anthropic/claude-sonnet-5-5.yaml",
		"subscription: claude-sub\nuse: allowed\nvision: yes\nefforts: low, high\nfrom: the claude-sub account\nfound: 2026-09-01\n"))
	global := layerOf("global", oneFile("models/anthropic/claude-sonnet-5-5.yaml", "use: excluded\nreason: not on this machine\n"))
	library, err := Load([]Layer{shippedLayer(), catalog, global})
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	model, _ := library.Resolve("claude-sub/claude-sonnet-5-5")
	if model.Use != UseExcluded || model.Reason != "not on this machine" {
		t.Fatalf("the global file did not win on use: %+v", model)
	}
	if !slices.Equal(model.Efforts, []llm.Effort{llm.EffortLow, llm.EffortHigh}) || model.Vision != VisionSees ||
		model.From != "the claude-sub account" || model.Found != "2026-09-01" {
		t.Fatalf("the global file replaced the catalog entry whole: %+v", model)
	}
	if model.Layer != "global" || !strings.HasPrefix(model.File, "global") {
		t.Fatalf("the entry reports layer %q file %q, want the global file that last wrote it", model.Layer, model.File)
	}
}

func TestFromAndFoundRoundTripThroughTheCatalog(t *testing.T) {
	library := shippedLibrary(t)
	registry := registryOf(t, `{"anthropic":{"models":{"claude-mythos-1":{"tool_call":true,"reasoning":true,`+
		`"reasoning_options":[{"type":"budget_tokens","min":1024},{"type":"effort","values":["low","ultra"]}],`+
		`"modalities":{"input":["text","image"]},"limit":{"context":200000}}}}}`)
	plan := planOf(library, ClaudeSub, registry, "claude-mythos-1", "claude-sonnet-5-5")
	dir := t.TempDir()
	if err := plan.Write(dir); err != nil {
		t.Fatalf("writing: %v", err)
	}
	reloaded, err := Load([]Layer{shippedLayer(), sys.DirLayer(catalogLayer, dir)})
	if err != nil {
		t.Fatalf("the written catalog does not load: %v", err)
	}
	for _, wrote := range plan {
		read, found := reloaded.Resolve(wrote.Slug())
		if !found || read.From != wrote.From || read.Found != wrote.Found || read.Layer != catalogLayer ||
			read.Use != wrote.Use || !slices.Equal(read.Efforts, wrote.Efforts) || read.Vision != wrote.Vision {
			t.Fatalf("wrote %+v, read back %+v", wrote, read)
		}
	}
	mythos := entryIn(t, plan, "claude-sub/claude-mythos-1")
	if !slices.Equal(mythos.Efforts, []llm.Effort{llm.EffortLow}) || mythos.Vision != VisionSees || !strings.Contains(mythos.From, "models.dev") {
		t.Fatalf("a new family took %v %q from %q, want low alone and sight from models.dev", mythos.Efforts, mythos.Vision, mythos.From)
	}
	if unknown := reloaded.Reconcile(Served{Subscription: ClaudeSub, IDs: []string{"claude-mythos-1", "claude-sonnet-5-5"}}, registry).Unknown; len(unknown) != 0 {
		t.Fatalf("the catalog entries are still served and not in the library: %v", unknown)
	}
	var files []string
	_ = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, _ error) error {
		if !entry.IsDir() {
			rel, _ := filepath.Rel(dir, path)
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	slices.Sort(files)
	if !slices.Equal(files, []string{"models/anthropic/claude-mythos-1.yaml", "models/anthropic/claude-sonnet-5-5.yaml"}) {
		t.Fatalf("the catalog holds %v", files)
	}
}

func TestWriteRefusesAnIdThatLeavesTheCatalog(t *testing.T) {
	root := t.TempDir()
	for _, id := range []string{"../escape", `..\escape`, "a/b", ".."} {
		plan := CatalogPlan{{Provider: Anthropic, ID: id, Subscription: ClaudeSub, Use: UseAllowed}}
		if err := plan.Write(filepath.Join(root, "catalog")); err == nil {
			t.Fatalf("an id %q was written", id)
		}
	}
	if entries, _ := os.ReadDir(root); len(entries) != 0 {
		t.Fatalf("a refused plan left %v behind", entries)
	}
}

func TestWriteReplacesACatalogFileWholeRatherThanRewritingItInPlace(t *testing.T) {
	dir := t.TempDir()
	written := filepath.Join(dir, modelsDir, string(Anthropic), "claude-mythos-1.yaml")
	plan := CatalogPlan{{Provider: Anthropic, ID: "claude-mythos-1", Subscription: ClaudeSub, Use: UseAllowed}}
	if err := plan.Write(dir); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(written)
	alias := filepath.Join(dir, "alias.yaml")
	if err := os.Link(written, alias); err != nil {
		t.Skipf("this filesystem makes no hard link: %v", err)
	}
	plan[0].Use, plan[0].Reason = UseExcluded, "the account no longer serves it"
	if err := plan.Write(dir); err != nil {
		t.Fatal(err)
	}
	if kept, _ := os.ReadFile(alias); string(kept) != string(before) {
		t.Fatalf("the old file was rewritten in place, so a quit halfway leaves it half written: it now reads %q", kept)
	}
	if after, _ := os.ReadFile(written); !strings.Contains(string(after), "use: excluded") {
		t.Fatalf("the new file reads %q", after)
	}
}

func TestATemporaryFileLeftByAQuitWriteDoesNotBreakTheNextLoad(t *testing.T) {
	dir := t.TempDir()
	plan := CatalogPlan{{Provider: Anthropic, ID: "claude-mythos-1", Subscription: ClaudeSub, Use: UseAllowed}}
	if err := plan.Write(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, modelsDir, string(Anthropic), ".tofu-4071"), []byte("subscr"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load([]Layer{shippedLayer(), sys.DirLayer(catalogLayer, dir)}); err != nil {
		t.Fatalf("a temporary file a quit write left behind breaks the next start: %v", err)
	}
}

func TestModelsDevFactsAreReadAndAnAbsentToolCallIsNotAFalseOne(t *testing.T) {
	registry := registryOf(t, `{"anthropic":{"models":{`+
		`"sees":{"tool_call":true,"reasoning":true,"reasoning_options":[{"type":"budget_tokens","min":1024},{"type":"effort","values":["low","high"]}],"modalities":{"input":["text","image"]},"limit":{"context":1}},`+
		`"silent":{"limit":{"context":1}},`+
		`"toolless":{"tool_call":false,"limit":{"context":1}}}}}`)
	sees, known := registry.Fact("anthropic/sees-20261001")
	if !known || !sees.ToolCalls || !sees.Reasoning || !sees.Images || !slices.Equal(sees.Efforts, []string{"low", "high"}) {
		t.Fatalf("the facts for a full entry came back %+v %v", sees, known)
	}
	if _, known := registry.Fact("anthropic/silent"); known {
		t.Fatal("an entry that says nothing about tool calls came back as a fact")
	}
	if toolless, known := registry.Fact("anthropic/toolless"); !known || toolless.ToolCalls {
		t.Fatalf("an entry marked without tool calls came back %+v %v", toolless, known)
	}
	path := filepath.Join(t.TempDir(), "registry.json")
	if err := registry.Store(path); err != nil {
		t.Fatalf("storing: %v", err)
	}
	stored, err := RegistryAt(path)
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if again, _ := stored.Fact("anthropic/sees"); !slices.Equal(again.Efforts, sees.Efforts) || !again.Images {
		t.Fatalf("a stored registry lost its facts: %+v", again)
	}
	shipped, known := shippedTable(t).Fact("anthropic/claude-sonnet-5")
	if !known || !shipped.ToolCalls || len(shipped.Efforts) == 0 || !shipped.Images {
		t.Fatalf("the shipped snapshot carries no facts for claude-sonnet-5: %+v %v", shipped, known)
	}
}

func TestLayersPutTheCatalogBetweenTheShippedFilesAndTheHome(t *testing.T) {
	layers, err := Layers(fstest.MapFS{}, t.TempDir())
	if err != nil {
		t.Fatalf("layers: %v", err)
	}
	names := make([]string, len(layers))
	for i, layer := range layers {
		names[i] = layer.Name
	}
	if !slices.Equal(names, []string{"library", catalogLayer, "global", "project"}) {
		t.Fatalf("the layers are %v", names)
	}
}

func TestAnUnknownSlugSaysHowToReload(t *testing.T) {
	_, err := shippedLibrary(t).Select("claude-sub/claude-sonnet-5-5")
	var refusal *Refusal
	if !errors.As(err, &refusal) || !strings.Contains(refusal.Error(), ReloadVerb) {
		t.Fatalf("the refusal does not say to run %s: %v", ReloadVerb, err)
	}
}
