package rule

import (
	"testing"
	"time"
)

func TestOverrideRateCountsOnlyBlockedFires(t *testing.T) {
	var ledger Ledger
	ledger.Append(Fire{RuleID: "em_dash", Mode: ModeShadow, Blocked: false, At: time.Now()})
	blockedOverridden := ledger.Append(Fire{RuleID: "em_dash", Mode: ModeEnforced, Blocked: true, At: time.Now()})
	ledger.Append(Fire{RuleID: "em_dash", Mode: ModeEnforced, Blocked: true, At: time.Now()})
	ledger.Append(Fire{RuleID: "em_dash", Mode: ModeEnforced, Blocked: true, At: time.Now()})
	ledger.Override(blockedOverridden)

	overridden, blocked := ledger.OverrideRate()
	if blocked != 3 {
		t.Fatalf("blocked = %d, want 3, the shadow fire must not count", blocked)
	}
	if overridden != 1 {
		t.Fatalf("overridden = %d, want 1", overridden)
	}
}

func TestOwnershipCheckerWrapsCrewMatches(t *testing.T) {
	r := Rule{ID: "ownership", Kind: KindStructural, Checker: "ownership", Mode: ModeShadow}
	covered := OwnsWrite{Path: "internal/rule/ledger.go", Owns: []string{"internal/rule/**"}}
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
