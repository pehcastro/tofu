package fire

import (
	"path/filepath"
	"testing"
)

func TestStructuralCatalogFindsExactlySevenRules(t *testing.T) {
	dir := filepath.Join(repoRoot, "library")
	catalog, err := StructuralCatalog(dir)
	if err != nil {
		t.Fatalf("StructuralCatalog: %v", err)
	}
	if len(catalog) != 7 {
		t.Fatalf("structural rules = %d, want 7: %+v", len(catalog), catalog)
	}
	want := []string{"comments", "em_dash", "no_worktree", "ownership", "test_assertion", "test_boundary_cases", "test_mock_boundary"}
	for i, id := range want {
		if catalog[i].ID != id {
			t.Fatalf("catalog[%d].ID = %q, want %q", i, catalog[i].ID, id)
		}
		if catalog[i].Mode != "shadow" {
			t.Fatalf("%s mode = %q, want shadow; a mode moved and this report is stale", catalog[i].ID, catalog[i].Mode)
		}
	}
}
