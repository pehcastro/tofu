package calibration

import (
	"path/filepath"
	"testing"
)

const repoRoot = "../.."

func TestTheLedgerHasBeenCounted(t *testing.T) {
	dir := filepath.Join(repoRoot, ".tofu", "log")
	counts, err := Count(dir)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}

	if counts.TotalRows != 2780 {
		t.Fatalf("total rows = %d, want 2780; the ledger moved and this report needs a rerun", counts.TotalRows)
	}
	if counts.OutcomeByKind["hand-labeled"] != 20 {
		t.Fatalf("hand-labeled outcomes = %d, want 20", counts.OutcomeByKind["hand-labeled"])
	}
	if len(counts.OutcomeByKind) != 1 {
		t.Fatalf("outcome kinds = %v, want only hand-labeled", counts.OutcomeByKind)
	}

	toolGate, ok := counts.Points["tool_gate"]
	if !ok {
		t.Fatal("no tool_gate rows in the ledger")
	}
	if toolGate.Rows != 1458 {
		t.Fatalf("tool_gate rows = %d, want 1458", toolGate.Rows)
	}
	if toolGate.Labelled != 20 {
		t.Fatalf("tool_gate labelled = %d, want 20", toolGate.Labelled)
	}
	if toolGate.NearThreshold["risk_ask_at"] != 0 {
		t.Fatalf("tool_gate labelled rows near risk_ask_at = %d, want 0", toolGate.NearThreshold["risk_ask_at"])
	}
	if toolGate.NearThreshold["risk_deny_at"] != 1 {
		t.Fatalf("tool_gate labelled rows near risk_deny_at = %d, want 1", toolGate.NearThreshold["risk_deny_at"])
	}

	stopCheck, ok := counts.Points["stop_check"]
	if !ok {
		t.Fatal("no stop_check rows in the ledger")
	}
	if stopCheck.Rows != 1105 {
		t.Fatalf("stop_check rows = %d, want 1105", stopCheck.Rows)
	}
	if stopCheck.Labelled != 0 {
		t.Fatalf("stop_check labelled = %d, want 0", stopCheck.Labelled)
	}

	shellSift, ok := counts.Points["shell_sift"]
	if !ok {
		t.Fatal("no shell_sift rows in the ledger")
	}
	if shellSift.Rows != 13 {
		t.Fatalf("shell_sift rows = %d, want 13", shellSift.Rows)
	}
	if shellSift.Labelled != 0 {
		t.Fatalf("shell_sift labelled = %d, want 0", shellSift.Labelled)
	}

	if _, ok := counts.Points["ask"]; ok {
		t.Fatal("the ledger now carries an ask row; the ask point can be added to the calibratable set")
	}

	sum := 0
	for _, pc := range counts.Points {
		sum += pc.Rows
	}
	if sum != counts.TotalRows {
		t.Fatalf("per point rows sum to %d, ledger total is %d", sum, counts.TotalRows)
	}

	rate := RateFrom(counts.OutcomeTimes)
	if rate.Count != 20 {
		t.Fatalf("rate count = %d, want 20", rate.Count)
	}
	if rate.Span >= 24*3600*1e9 {
		t.Fatalf("outcome span = %s, want under a day: the labels arrived in one burst", rate.Span)
	}
}

func TestFloorsMatchTheRuleOfThree(t *testing.T) {
	if got := RuleOfThree(BudgetFivePercent); got != 60 {
		t.Fatalf("RuleOfThree(0.05) = %d, want 60", got)
	}
	if got := RuleOfThree(BudgetOnePercent); got != 300 {
		t.Fatalf("RuleOfThree(0.01) = %d, want 300", got)
	}
}

func TestFurtherNeverGoesNegative(t *testing.T) {
	if got := Further(80, 60); got != 0 {
		t.Fatalf("Further(80, 60) = %d, want 0", got)
	}
	if got := Further(1, 60); got != 59 {
		t.Fatalf("Further(1, 60) = %d, want 59", got)
	}
}

func TestReachIsThreeDeadBands(t *testing.T) {
	if got := ReachBand(); got < 0.179 || got > 0.181 {
		t.Fatalf("ReachBand() = %v, want 0.18", got)
	}
	if !Near(2.35, 2.5) {
		t.Fatal("2.35 should sit within reach of a threshold of 2.5")
	}
	if Near(2.0, 2.5) {
		t.Fatal("2.0 should sit outside reach of a threshold of 2.5")
	}
}
