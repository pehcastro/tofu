package rule

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeEmDashViolation(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "violation.txt")
	content := "one line is fine\nthe other line breaks the rule" + string(rune(0x2014)) + "on purpose\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	return path
}

func TestShadowRecordsAFireAndBlocksNothing(t *testing.T) {
	path := writeEmDashViolation(t)
	r := Rule{ID: "em_dash", Kind: KindStructural, Checker: "em_dash", Mode: ModeShadow, ModeDeclared: true}
	fire, err := Run(r, Builtins(), TextFile{Path: path}, path, time.Now())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(fire.Findings) == 0 {
		t.Fatal("no findings on a file that violates the rule")
	}
	if fire.Blocked {
		t.Fatal("Blocked = true in shadow mode")
	}
}

func TestEnforcedBlocksTheSameRuleAndTheRecordDistinguishesIt(t *testing.T) {
	path := writeEmDashViolation(t)
	shadowRule := Rule{ID: "em_dash", Kind: KindStructural, Checker: "em_dash", Mode: ModeShadow, ModeDeclared: true}
	enforcedRule := Rule{ID: "em_dash", Kind: KindStructural, Checker: "em_dash", Mode: ModeEnforced, ModeDeclared: true}

	shadowFire, err := Run(shadowRule, Builtins(), TextFile{Path: path}, path, time.Now())
	if err != nil {
		t.Fatalf("Run shadow: %v", err)
	}
	enforcedFire, err := Run(enforcedRule, Builtins(), TextFile{Path: path}, path, time.Now())
	if err != nil {
		t.Fatalf("Run enforced: %v", err)
	}

	if shadowFire.Blocked {
		t.Fatal("the shadow run blocked")
	}
	if !enforcedFire.Blocked {
		t.Fatal("the enforced run did not block a file that violates the rule")
	}
	if len(shadowFire.Findings) != len(enforcedFire.Findings) {
		t.Fatalf("finding counts differ: shadow %d, enforced %d, the rule should be the same", len(shadowFire.Findings), len(enforcedFire.Findings))
	}
	if shadowFire.Mode == enforcedFire.Mode {
		t.Fatal("the two runs carry the same mode, they are not distinguishable in the record")
	}
}

func TestOwnershipCheckerWrapsCrewMatches(t *testing.T) {
	r := Rule{ID: "ownership", Kind: KindStructural, Checker: "ownership", Mode: ModeShadow}
	covered := OwnsWrite{Path: "internal/rule/run.go", Owns: []string{"internal/rule/**"}}
	fire, err := Run(r, Builtins(), covered, covered.Path, time.Now())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(fire.Findings) != 0 {
		t.Fatalf("a path inside owns produced a finding: %+v", fire.Findings)
	}

	uncovered := OwnsWrite{Path: "cmd/boji/bench.go", Owns: []string{"internal/rule/**"}}
	fire, err = Run(r, Builtins(), uncovered, uncovered.Path, time.Now())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(fire.Findings) != 1 {
		t.Fatalf("a path outside owns produced %d findings, want 1", len(fire.Findings))
	}
}

func TestRunFailsOnAnUnregisteredChecker(t *testing.T) {
	r := Rule{ID: "ghost", Kind: KindStructural, Checker: "does_not_exist", Mode: ModeShadow}
	_, err := Run(r, Builtins(), TextFile{Path: "x"}, "x", time.Now())
	if err == nil {
		t.Fatal("Run returned no error for a checker that is not registered")
	}
}
