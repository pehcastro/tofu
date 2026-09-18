package harness

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var updateGolden = flag.Bool("update", false, "update golden files")

func gatesOK() []GateResult {
	return []GateResult{
		{Name: "build", Status: GateStatusPassed, Reason: "ok"},
		{Name: "lint", Status: GateStatusPassed, Reason: "ok"},
		{Name: "test", Status: GateStatusPassed, Reason: "ok"},
	}
}

func gatesFailing(name string) []GateResult {
	return []GateResult{
		{Name: "build", Status: GateStatusPassed, Reason: "ok"},
		{Name: name, Status: GateStatusFailed, Reason: "boom"},
	}
}

func checklistFullPass() []ChecklistResult {
	return []ChecklistResult{{Item: "returns 400 on missing field", Passed: true}}
}

func dollarPtr(v float64) *float64 { return &v }

func routesFixture() []Row {
	return []Row{
		{
			Arm: ArmBoji, Task: "hono-routes", Version: 1, Run: 1,
			CredentialKind: CredentialKindKey,
			Gates:          gatesFailing("build"),
			Checklist:      checklistFullPass(),
		},
		{
			Arm: ArmBoji, Task: "hono-routes", Version: 1, Run: 2,
			CredentialKind: CredentialKindKey,
			WallClockMS:    100000,
			Dollars:        dollarPtr(0.11),
			Turns:          4,
			Gates:          gatesOK(),
			Checklist:      checklistFullPass(),
		},
		{
			Arm: ArmClaude, Task: "hono-routes", Version: 1, Run: 1,
			CredentialKind: CredentialKindSubscription,
			WallClockMS:    120000,
			Turns:          6,
			Gates:          gatesOK(),
			Checklist:      checklistFullPass(),
		},
		{
			Arm: ArmClaude, Task: "hono-routes", Version: 1, Run: 2,
			CredentialKind: CredentialKindSubscription,
			WallClockMS:    124000,
			Turns:          6,
			Gates:          gatesOK(),
			Checklist: []ChecklistResult{
				{Item: "returns 400 on missing field", Passed: true},
				{Item: "handles empty body", Passed: false},
			},
		},
		{
			Arm: ArmCodex, Task: "hono-routes", Version: 1, Run: 1,
			CredentialKind: CredentialKindKey,
			WallClockMS:    90000,
			Dollars:        dollarPtr(0.10),
			Turns:          4,
			Gates:          gatesOK(),
			Checklist:      checklistFullPass(),
		},
		{
			Arm: ArmCodex, Task: "hono-routes", Version: 1, Run: 2,
			CredentialKind: CredentialKindKey,
			WallClockMS:    95000,
			Dollars:        dollarPtr(0.12),
			Turns:          5,
			Gates:          gatesOK(),
			Checklist:      checklistFullPass(),
		},
	}
}

func TestRenderGolden(t *testing.T) {
	got := Render(routesFixture())
	path := filepath.Join("testdata", "report", "hono-routes.golden")

	if *updateGolden {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if got != string(want) {
		t.Fatalf("render does not match golden %s\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}

func TestGateFailureExcludedButShown(t *testing.T) {
	out := Render(routesFixture())

	if !strings.Contains(out, "boji v1 run1: FAIL build") {
		t.Fatalf("failing row missing from gates door:\n%s", out)
	}
	if !strings.Contains(out, "boji: $0.1100 (spread $0.0000, 1 runs)") {
		t.Fatalf("failed run1 leaked into boji's dollars-per-passing-run figure, only run2 must count:\n%s", out)
	}
	if !strings.Contains(out, "boji: 4.00 (spread 0.00, 1 runs)") {
		t.Fatalf("failed run1 leaked into boji's turns-per-passing-run figure, only run2 must count:\n%s", out)
	}
	if strings.Count(out, "boji:") != 3 {
		t.Fatalf("want exactly 3 non-gate lines naming boji (dollars, turns, checklist), the failed run1 must not add a fourth:\n%s", out)
	}
}

func TestMixingCredentialKindsRefusesDollars(t *testing.T) {
	out := Render(routesFixture())

	want := "boji vs claude: dollars not comparable, credential kinds differ (key vs subscription)"
	if !strings.Contains(out, want) {
		t.Fatalf("missing refusal line %q:\n%s", want, out)
	}
	if strings.Contains(out, "boji vs claude: $") {
		t.Fatalf("a dollar figure was printed for a mixed-kind comparison instead of a refusal:\n%s", out)
	}
}

func TestSubscriptionDollarsAreAbsentNotZero(t *testing.T) {
	out := Render(routesFixture())

	if !strings.Contains(out, "claude: n/a (subscription)") {
		t.Fatalf("subscription arm's dollars did not render as absent:\n%s", out)
	}
	if strings.Contains(out, "claude: $0.0000") {
		t.Fatalf("subscription arm's dollars rendered as a zero, indistinguishable from a real zero spend:\n%s", out)
	}
}

func TestNoWeightedScoreTotalOrPercentageInOutput(t *testing.T) {
	out := strings.ToLower(Render(routesFixture()))

	for _, word := range []string{"score", "weighted", "total", "%"} {
		if strings.Contains(out, word) {
			t.Fatalf("output contains forbidden term %q:\n%s", word, out)
		}
	}
}

func TestDifferenceSmallerThanSpreadIsNoDifference(t *testing.T) {
	out := Render(routesFixture())

	want := "boji vs codex: no difference (dollars diff $0.0000, spread $0.0200)"
	if !strings.Contains(out, want) {
		t.Fatalf("missing no-difference line %q:\n%s", want, out)
	}
}

func TestRealDifferenceIsReportedWithSpread(t *testing.T) {
	out := Render(routesFixture())

	want := "claude vs codex: codex lower on turns by 1.50 (spread 1.00)"
	if !strings.Contains(out, want) {
		t.Fatalf("missing real difference line %q:\n%s", want, out)
	}
}
