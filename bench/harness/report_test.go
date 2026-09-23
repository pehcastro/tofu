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

func passingRepeats(task string, arm Arm, kind CredentialKind, wall, turns []int64, dollars []float64) []Row {
	rows := make([]Row, 0, len(turns))
	for i := range turns {
		row := Row{
			Arm: arm, Task: task, Version: 1, Run: i + 1,
			CredentialKind: kind,
			WallClockMS:    wall[i],
			Turns:          turns[i],
			Gates:          gatesOK(),
			Checklist:      checklistFullPass(),
		}
		if len(dollars) > 0 {
			row.ModelDollars = dollarPtr(dollars[i])
		}
		rows = append(rows, row)
	}
	return rows
}

func tofuRoutesRepeats() []Row {
	return passingRepeats("hono-routes", ArmTofu, CredentialKindKey,
		[]int64{100000, 102000, 101000}, []int64{4, 5, 6}, []float64{0.11, 0.11, 0.12})
}

func codexRoutesRepeats() []Row {
	return passingRepeats("hono-routes", ArmCodex, CredentialKindKey,
		[]int64{90000, 95000, 92000}, []int64{4, 5, 4}, []float64{0.10, 0.12, 0.11})
}

func routesFixture() []Row {
	rows := append(tofuRoutesRepeats(), passingRepeats("hono-routes", ArmClaude, CredentialKindSubscription,
		[]int64{120000, 124000, 121000}, []int64{6, 6, 7}, nil)...)
	return append(rows, codexRoutesRepeats()...)
}

func failingEveryRepeat() []Row {
	rows := tofuRoutesRepeats()
	for i := range rows {
		rows[i].Gates = gatesFailing("build")
	}
	return append(rows, codexRoutesRepeats()...)
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
	out := Render(failingEveryRepeat())

	if !strings.Contains(out, "tofu v1 run1: FAIL build") {
		t.Fatalf("failing row missing from gates door:\n%s", out)
	}
	if strings.Contains(out, "UNSTABLE") {
		t.Fatalf("an arm that failed every repeat is not unstable, it is stably failing:\n%s", out)
	}
	if strings.Count(out, "\ntofu:") != 0 {
		t.Fatalf("an arm whose every repeat failed the gate was ranked anyway:\n%s", out)
	}
	if !strings.Contains(out, "codex: model median $0.1100, range $0.1000 to $0.1200 over 3 repeats") {
		t.Fatalf("the passing arm lost its median and range:\n%s", out)
	}
}

func TestMixingCredentialKindsRefusesDollars(t *testing.T) {
	out := Render(routesFixture())

	want := "claude vs tofu: dollars not comparable, credential kinds differ (subscription vs key)"
	if !strings.Contains(out, want) {
		t.Fatalf("missing refusal line %q:\n%s", want, out)
	}
	if strings.Contains(out, "claude vs tofu: $") {
		t.Fatalf("a dollar figure was printed for a mixed-kind comparison instead of a refusal:\n%s", out)
	}
}

func TestSubscriptionDollarsAreAbsentNotZero(t *testing.T) {
	out := Render(routesFixture())

	if !strings.Contains(out, "claude: model n/a, spent as subscription quota") {
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

	want := "codex vs tofu: no difference (dollars diff $0.0000, widest range $0.0200)"
	if !strings.Contains(out, want) {
		t.Fatalf("missing no-difference line %q:\n%s", want, out)
	}
}

func armWithNoTurnsRecorded() []Row {
	return []Row{
		{
			Arm: ArmTofu, Task: "crashed-before-any-turn", Version: 2, Run: 1,
			CredentialKind: CredentialKindSubscription,
			WallClockMS:    30000,
			Turns:          0,
			Gates:          gatesOK(),
			Checklist:      checklistFullPass(),
		},
		{
			Arm: ArmClaude, Task: "crashed-before-any-turn", Version: 2, Run: 1,
			CredentialKind: CredentialKindSubscription,
			WallClockMS:    120000,
			Turns:          14,
			Gates:          gatesOK(),
			Checklist:      checklistFullPass(),
		},
	}
}

func TestAnArmThatRecordedNoTurnsIsNotRanked(t *testing.T) {
	out := Render(armWithNoTurnsRecorded())

	if !strings.Contains(out, "tofu: turns not recorded on any passing run") {
		t.Fatalf("a row with zero turns rendered as a measurement:\n%s", out)
	}
	if !strings.Contains(out, "claude vs tofu: turns not comparable, tofu recorded none") {
		t.Fatalf("missing the refusal to compare against an unmeasured arm:\n%s", out)
	}
	if strings.Contains(out, "lower on turns") {
		t.Fatalf("an arm was ranked on turns it never recorded:\n%s", out)
	}
}

func TestRealDifferenceIsReportedWithSpread(t *testing.T) {
	out := Render(routesFixture())

	want := "claude vs codex: codex lower on turns by 2.00 (widest range 1.00)"
	if !strings.Contains(out, want) {
		t.Fatalf("missing real difference line %q:\n%s", want, out)
	}
}

func subscriptionRepeats(task string, arm Arm, wall, turns []int64) []Row {
	return passingRepeats(task, arm, CredentialKindSubscription, wall, turns, nil)
}

func TestMedianAndRangeOverThreeRepeats(t *testing.T) {
	rows := append(
		subscriptionRepeats("hono-repeats", ArmClaude, []int64{120000, 124000, 121000}, []int64{6, 6, 7}),
		subscriptionRepeats("hono-repeats", ArmCodex, []int64{90000, 95000, 92000}, []int64{4, 5, 4})...)

	out := Render(rows)

	if !strings.Contains(out, "median 6.00, range 6.00 to 7.00 over 3 repeats") {
		t.Fatalf("no median and range over the three repeats:\n%s", out)
	}
}

func TestOneRepeatSaysItHasNoSpread(t *testing.T) {
	rows := append(
		subscriptionRepeats("hono-once", ArmClaude, []int64{120000}, []int64{6}),
		subscriptionRepeats("hono-once", ArmCodex, []int64{90000}, []int64{4})...)

	out := Render(rows)

	if !strings.Contains(out, "one repeat, which is a sample and not a spread") {
		t.Fatalf("a single repeat was printed as if it carried a spread:\n%s", out)
	}
	if strings.Contains(out, "lower on turns") {
		t.Fatalf("two arms of one repeat each were separated, which one run cannot do:\n%s", out)
	}
}

func TestATaskUnstableAcrossRepeatsRanksNoArm(t *testing.T) {
	rows := append(
		subscriptionRepeats("hono-unstable", ArmClaude, []int64{120000, 124000, 121000}, []int64{6, 6, 7}),
		subscriptionRepeats("hono-unstable", ArmCodex, []int64{90000, 95000, 92000}, []int64{4, 5, 4})...)
	rows[4].Gates = gatesFailing("build")

	out := Render(rows)

	if !strings.Contains(out, "UNSTABLE hono-unstable") {
		t.Fatalf("an arm that passed on two repeats and failed on one was not named unstable:\n%s", out)
	}
	if strings.Contains(out, "TURNS PER PASSING RUN hono-unstable") {
		t.Fatalf("an unstable task ranked an arm anyway:\n%s", out)
	}
}

func realRun20260923() []Row {
	return []Row{
		{
			Arm: ArmClaude, Task: "hono-v1", Version: 1, Run: 1,
			CredentialKind: CredentialKindSubscription,
			WallClockMS:    51204, Turns: 8,
			Gates:     gatesOK(),
			Checklist: checklistFullPass(),
		},
		{
			Arm: ArmCodex, Task: "hono-v1", Version: 1, Run: 1,
			CredentialKind: CredentialKindSubscription,
			WallClockMS:    285046, Turns: 1,
			Gates:     gatesOK(),
			Checklist: checklistFullPass(),
		},
	}
}

func TestOneRepeatFromTheReal20260923RowIsOneSampleNotASpread(t *testing.T) {
	out := Render(realRun20260923())

	for _, want := range []string{
		"claude: wall clock 51204 ms on one repeat, which is a sample and not a spread",
		"codex: wall clock 285046 ms on one repeat, which is a sample and not a spread",
		"claude: 8.00 on one repeat, which is a sample and not a spread",
		"codex: 1.00 on one repeat, which is a sample and not a spread",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing one-sample line %q from the real 2026-09-23 row data:\n%s", want, out)
		}
	}
}

func TestSuppressedComparisonSentenceOnTheReal20260923Row(t *testing.T) {
	out := Render(realRun20260923())

	for _, want := range []string{
		"claude vs codex: wall clock not separable, claude and codex carry fewer than two passing repeats and one run has no spread",
		"claude vs codex: turns not separable, claude and codex carry fewer than two passing repeats and one run has no spread",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing suppressed comparison sentence %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "lower on wall clock") || strings.Contains(out, "lower on turns") {
		t.Fatalf("two one-repeat arms were separated into a win, which one run each cannot support:\n%s", out)
	}
}

func TestRowsSkippedForAMissingFieldAreCountedAndNamed(t *testing.T) {
	rows := subscriptionRepeats("hono-missing", ArmClaude, []int64{120000, 124000, 121000}, []int64{6, 6, 7})
	rows[1].CredentialKind = ""
	rows[2].Gates = nil

	out := Render(rows)

	if !strings.Contains(out, "2 rows are not read") {
		t.Fatalf("rows missing a field were not counted:\n%s", out)
	}
	if !strings.Contains(out, "claude v1 run2 has no credential kind") || !strings.Contains(out, "claude v1 run3 has no gate result") {
		t.Fatalf("rows missing a field were not named:\n%s", out)
	}
}
