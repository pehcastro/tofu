package models

import (
	"os"
	"strings"
	"testing"

	"tofu/internal/golden"
	"tofu/internal/llm"
	library "tofu/internal/llm/models"
)

const (
	shippedRoot = "../../../library"
	claudeSub   = library.Subscription("claude-sub")
)

func shippedLibrary(t *testing.T) library.Library {
	t.Helper()
	layer := library.Layer{Name: "library", Origin: "library", FS: os.DirFS(shippedRoot)}
	loaded, err := library.Load([]library.Layer{layer})
	if err != nil {
		t.Fatalf("loading the shipped library: %v", err)
	}
	return loaded
}

func sourceEfforts(id library.Subscription) []llm.Effort {
	if id == claudeSub {
		return []llm.Effort{llm.EffortLow, llm.EffortMedium, llm.EffortHigh, llm.EffortXHigh, llm.EffortMax}
	}
	return llm.Efforts()
}

func everySource(loaded library.Library) []Source {
	sources := make([]Source, 0, len(loaded.Subscriptions))
	for _, spec := range loaded.Subscriptions {
		sources = append(sources, Source{ID: spec.ID, Efforts: sourceEfforts(spec.ID)})
	}
	return sources
}

func shippedPicker(t *testing.T) Model {
	t.Helper()
	loaded := shippedLibrary(t)
	built := Build(loaded, everySource(loaded))
	built.SetSize(120, 36)
	return built
}

func TestEveryRowSpellsSourceSlashModel(t *testing.T) {
	loaded := shippedLibrary(t)
	if len(loaded.Models) == 0 {
		t.Fatal("the shipped library carries no model")
	}
	type paidFor struct {
		source library.Subscription
		id     string
	}
	known := map[paidFor]bool{}
	for _, one := range loaded.Models {
		known[paidFor{one.Subscription, one.ID}] = true
	}
	for _, group := range Build(loaded, everySource(loaded)).Groups {
		for _, row := range group.Rows {
			source, name, found := strings.Cut(row.Slug, "/")
			if !found {
				t.Fatalf("%s has no slash", row.Slug)
			}
			if source != group.Source {
				t.Errorf("%s: the source is %q, want the subscription %q that pays for it", row.Slug, source, group.Source)
			}
			if !known[paidFor{library.Subscription(source), name}] {
				t.Errorf("%s: the library has no %q under the %q subscription, and a slug names the money", row.Slug, name, source)
			}
		}
	}
}

func TestBuildGroupsBySubscriptionInLibraryOrder(t *testing.T) {
	built := shippedPicker(t)
	if len(built.Groups) == 0 {
		t.Fatal("no group built from a non-empty library")
	}
	for _, group := range built.Groups {
		for _, row := range group.Rows {
			if !strings.HasPrefix(row.Slug, group.Source+"/") {
				t.Errorf("%s sits under the %s group but does not start with %s/", row.Slug, group.Source, group.Source)
			}
		}
	}
}

func TestPickWalksEveryRowAndStopsAtTheEnds(t *testing.T) {
	built := shippedPicker(t)
	total := built.count()
	if total < 2 {
		t.Fatalf("the shipped library has %d models, need at least 2 to prove the walk stops", total)
	}
	for range total + 3 {
		built.Key("down")
	}
	if last, ok := built.Picked(); !ok || last.Slug == "" {
		t.Fatal("the pick ran past the last row")
	}
	for range total + 3 {
		built.Key("up")
	}
	if first, ok := built.Picked(); !ok || first.Slug == "" {
		t.Fatal("the pick ran past the first row")
	}
}

func TestPickerViewGolden(t *testing.T) {
	golden.Assert(t, "picker-120x36.golden", shippedPicker(t).View())
}

func TestAnExcludedModelCannotBePicked(t *testing.T) {
	loaded := shippedLibrary(t)
	excluded := 0
	for _, one := range loaded.Models {
		if one.Use == library.UseExcluded {
			excluded++
		}
	}
	if excluded == 0 {
		t.Skip("the shipped library excludes no model, so nothing here can prove an excluded model is unreachable")
	}
	built := shippedPicker(t)
	for _, key := range []string{"down", "up"} {
		for range built.count() + excluded {
			row, picked := built.Picked()
			if picked && row.Use == library.UseExcluded {
				t.Fatalf("the pick landed on %s, which the library excludes: %s", row.Slug, row.Reason)
			}
			built.Key(key)
		}
	}
}

func TestAnEffortOneSubscriptionRefusesFallsBackOnTheOther(t *testing.T) {
	built := shippedPicker(t)
	for range built.count() {
		if row, _ := built.Picked(); strings.HasPrefix(row.Slug, "codex-sub/") {
			break
		}
		built.Key("down")
	}
	if row, _ := built.Picked(); !strings.HasPrefix(row.Slug, "codex-sub/") {
		t.Skipf("no codex-sub row is offered, so the two effort lists cannot be crossed here")
	}
	for range len(llm.Efforts()) {
		built.Key("left")
	}
	if built.Effort() != llm.EffortNone {
		t.Fatalf("the codex-sub group offers %s and the walk stopped at %q", llm.EffortList(llm.Efforts()), built.Effort())
	}
	for range built.count() {
		if row, _ := built.Picked(); strings.HasPrefix(row.Slug, "claude-sub/") {
			break
		}
		built.Key("up")
	}
	if built.Effort() != llm.EffortDefault {
		t.Fatalf("the pick moved to a group that refuses %s and the effort stayed %q", llm.EffortNone, built.Effort())
	}
}

func TestEmptyPickerGolden(t *testing.T) {
	var empty Model
	empty.SetSize(120, 36)
	golden.Assert(t, "picker-empty-120x36.golden", empty.View())
}
