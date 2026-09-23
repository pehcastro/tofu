package rules

import (
	"path/filepath"
	"testing"
)

func realCatalogAndFires(t *testing.T) ([]CatalogRule, []Fire) {
	t.Helper()
	catalog, err := StructuralCatalog(filepath.Join(repoRoot, "library"))
	if err != nil {
		t.Fatalf("StructuralCatalog: %v", err)
	}
	fires, _, err := ReadDir(filepath.Join(repoRoot, ".tofu", "log"))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	return catalog, fires
}

func TestCountMatchesTheHandRead(t *testing.T) {
	catalog, fires := realCatalogAndFires(t)
	counts := Count(catalog, fires)
	if len(counts) != 7 {
		t.Fatalf("counts = %d, want 7", len(counts))
	}
	by := map[string]RuleCount{}
	for _, c := range counts {
		by[c.RuleID] = c
	}

	for _, id := range []string{"comments", "no_worktree", "ownership"} {
		c := by[id]
		if !c.NeverFired() {
			t.Fatalf("%s.NeverFired() = false, want true, fires = %d", id, c.Fires)
		}
	}

	em := by["em_dash"]
	if em.Fires != 25 {
		t.Fatalf("em_dash fires = %d, want 25", em.Fires)
	}
	if em.DistinctTargets != 8 {
		t.Fatalf("em_dash distinct targets = %d, want 8", em.DistinctTargets)
	}
	if em.Days != 3 {
		t.Fatalf("em_dash days = %d, want 3", em.Days)
	}
	if em.FirstDay != "2026-09-18" || em.LastDay != "2026-09-20" {
		t.Fatalf("em_dash span = %s..%s, want 2026-09-18..2026-09-20", em.FirstDay, em.LastDay)
	}

	assertion := by["test_assertion"]
	if assertion.Fires != 84 {
		t.Fatalf("test_assertion fires = %d, want 84", assertion.Fires)
	}
	if assertion.DistinctTargets != 26 {
		t.Fatalf("test_assertion distinct targets = %d, want 26", assertion.DistinctTargets)
	}

	boundary := by["test_boundary_cases"]
	if boundary.Fires != 120 {
		t.Fatalf("test_boundary_cases fires = %d, want 120", boundary.Fires)
	}
	if boundary.DistinctTargets != 38 {
		t.Fatalf("test_boundary_cases distinct targets = %d, want 38", boundary.DistinctTargets)
	}

	mock := by["test_mock_boundary"]
	if mock.Fires != 10 {
		t.Fatalf("test_mock_boundary fires = %d, want 10", mock.Fires)
	}
	if mock.DistinctTargets != 2 {
		t.Fatalf("test_mock_boundary distinct targets = %d, want 2, every fire is on the mocked and weak fixtures", mock.DistinctTargets)
	}

	total := 0
	for _, c := range counts {
		total += c.Fires
	}
	if total != len(fires) {
		t.Fatalf("sum of per rule fires = %d, total fires read = %d", total, len(fires))
	}
}

func TestCountOnNoFiresLeavesEveryRuleUnfired(t *testing.T) {
	catalog := []CatalogRule{{ID: "em_dash", Mode: "shadow"}}
	counts := Count(catalog, nil)
	if len(counts) != 1 {
		t.Fatalf("counts = %d, want 1", len(counts))
	}
	if !counts[0].NeverFired() {
		t.Fatal("NeverFired() = false on an empty fire list, want true")
	}
	if counts[0].Days != 0 || counts[0].DistinctTargets != 0 {
		t.Fatalf("empty count = %+v, want zero days and zero targets", counts[0])
	}
}
