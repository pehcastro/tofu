package fire

import (
	"path/filepath"
	"testing"
)

func TestStructuralCatalogFindsExactlyNineRules(t *testing.T) {
	dir := filepath.Join(repoRoot, "library")
	catalog, err := StructuralCatalog(dir)
	if err != nil {
		t.Fatalf("StructuralCatalog: %v", err)
	}
	want := []string{"comments", "em_dash", "go_regex_once", "no_worktree", "ownership", "test_assertion", "test_boundary_cases", "test_mock_boundary", "ts_regex_once"}
	if len(catalog) != len(want) {
		t.Fatalf("structural rules = %d, want %d: %+v", len(catalog), len(want), catalog)
	}
	for i, id := range want {
		if catalog[i].ID != id {
			t.Fatalf("catalog[%d].ID = %q, want %q", i, catalog[i].ID, id)
		}
		if catalog[i].Mode != "shadow" {
			t.Fatalf("%s mode = %q, want shadow; a mode moved and this report is stale", catalog[i].ID, catalog[i].Mode)
		}
	}
}
