package models

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	catalog "tofu/internal/llm/models"
)

var update = flag.Bool("update", false, "rewrite the golden files")

const shippedRoot = "../../../catalog"

func shippedCatalog(t *testing.T) catalog.Catalog {
	t.Helper()
	layer := catalog.Layer{Name: "catalog", Origin: "catalog", FS: os.DirFS(shippedRoot)}
	loaded, err := catalog.Load([]catalog.Layer{layer})
	if err != nil {
		t.Fatalf("loading the shipped catalog: %v", err)
	}
	return loaded
}

func TestEveryRowSpellsSourceSlashModel(t *testing.T) {
	loaded := shippedCatalog(t)
	if len(loaded.Models) == 0 {
		t.Fatal("the shipped catalog carries no model")
	}
	for _, one := range loaded.Models {
		slug := Slug(one)
		source, name, found := strings.Cut(slug, "/")
		if !found {
			t.Fatalf("%s has no slash", slug)
		}
		if source != string(one.Subscription) {
			t.Errorf("%s: the source is %q, want the subscription %q that pays for it, not the vendor %q",
				slug, source, one.Subscription, one.Provider)
		}
		if name != one.ID {
			t.Errorf("%s: the model name is %q, want %q", slug, name, one.ID)
		}
	}
}

func TestBuildGroupsBySubscriptionInCatalogOrder(t *testing.T) {
	built := Build(shippedCatalog(t))
	if len(built.Groups) == 0 {
		t.Fatal("no group built from a non-empty catalog")
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
	built := Build(shippedCatalog(t))
	built.SetSize(80, 24)
	total := built.count()
	if total < 2 {
		t.Fatalf("the shipped catalog has %d models, need at least 2 to prove the walk stops", total)
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

func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != got {
		t.Errorf("%s does not match the golden file\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

func TestPickerViewGolden(t *testing.T) {
	built := Build(shippedCatalog(t))
	built.SetSize(120, 36)
	assertGolden(t, "picker-120x36.golden", built.View())
}

func TestEmptyPickerGolden(t *testing.T) {
	var empty Model
	empty.SetSize(120, 36)
	assertGolden(t, "picker-empty-120x36.golden", empty.View())
}
