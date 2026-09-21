package rule

import (
	"path/filepath"
	"testing"
	"time"
)

func TestCheckTreeRunsEveryShippedRuleOverATreeThatIsNotThisRepository(t *testing.T) {
	rules, err := LoadDir(filepath.Join("..", "..", "catalog"))
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	fires, err := CheckTree(rules, Builtins(), filepath.Join("testdata", "tree"), time.Now)
	if err != nil {
		t.Fatalf("CheckTree: %v", err)
	}
	perRule := map[string]int{}
	for _, f := range fires {
		perRule[f.RuleID]++
		for _, finding := range f.Findings {
			t.Logf("%s  %s  %s", f.RuleID, finding.Target, finding.Detail)
		}
	}
	want := map[string]int{"test_assertion": 2, "test_mock_boundary": 2, "test_boundary_cases": 2}
	if len(perRule) != len(want) {
		t.Fatalf("fires came from %v, want only the three test rules", perRule)
	}
	for id, count := range want {
		if perRule[id] != count {
			t.Fatalf("%s fired on %d packages, want %d: %v", id, perRule[id], count, perRule)
		}
	}
}

func TestCheckTreeSkipsACheckerWhoseSubjectTheWalkNeverProduces(t *testing.T) {
	ownership := Rule{ID: "ownership", Kind: KindStructural, Checker: "ownership", Mode: ModeShadow}
	fires, err := CheckTree([]Rule{ownership}, Builtins(), filepath.Join("testdata", "tree"), time.Now)
	if err != nil {
		t.Fatalf("CheckTree: %v", err)
	}
	if len(fires) != 0 {
		t.Fatalf("a rule whose subject is a write, not a file, produced %d fires: %+v", len(fires), fires)
	}
}
