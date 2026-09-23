package rules

import (
	"strings"
	"testing"
)

func TestRenderNamesEveryNeverFiredRuleAndTheFalsePositive(t *testing.T) {
	catalog, fires := realCatalogAndFires(t)
	counts := Count(catalog, fires)
	body := Render("test-machine", "2026-09-23", counts, 0)

	for _, id := range []string{"comments", "no_worktree", "ownership"} {
		if !strings.Contains(body, id) {
			t.Fatalf("report does not name %s", id)
		}
	}
	if !strings.Contains(body, "bench/cost/report.go:189") {
		t.Fatal("report does not name the false positive target")
	}
	if !strings.Contains(body, "tofu working on tofu") {
		t.Fatal("report does not state the bias")
	}
	for _, id := range []string{"comments", "no_worktree", "ownership", "em_dash", "test_assertion", "test_boundary_cases", "test_mock_boundary"} {
		if !strings.Contains(body, "**"+id+"**:") {
			t.Fatalf("report has no one-line recommendation for %s", id)
		}
	}
}

func TestRenderOnNoFiresStillPrintsANeverFiredSection(t *testing.T) {
	catalog := []CatalogRule{{ID: "em_dash", Mode: "shadow"}}
	body := Render("m", "2026-01-01", Count(catalog, nil), 2)
	if !strings.Contains(body, "em_dash: 0 fires") {
		t.Fatalf("report = %q, want a stated 0 fires for em_dash", body)
	}
	if !strings.Contains(body, "2 line(s) unreadable") {
		t.Fatal("report does not surface the unreadable line count")
	}
}
