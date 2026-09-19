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
	var ledger Ledger
	ledger.Append(fire)
	if got := len(ledger.Fires()); got != 1 {
		t.Fatalf("ledger recorded %d fires, want 1", got)
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

	var ledger Ledger
	ledger.Append(shadowFire)
	ledger.Append(enforcedFire)
	fires := ledger.Fires()
	if fires[0].Mode != ModeShadow || fires[1].Mode != ModeEnforced {
		t.Fatalf("ledger did not keep the two modes apart: %+v", fires)
	}
	if fires[0].Blocked == fires[1].Blocked {
		t.Fatal("ledger rows do not distinguish shadow from enforced by Blocked")
	}
}

func TestRunFailsOnAnUnregisteredChecker(t *testing.T) {
	r := Rule{ID: "ghost", Kind: KindStructural, Checker: "does_not_exist", Mode: ModeShadow}
	_, err := Run(r, Builtins(), TextFile{Path: "x"}, "x", time.Now())
	if err == nil {
		t.Fatal("Run returned no error for a checker that is not registered")
	}
}
