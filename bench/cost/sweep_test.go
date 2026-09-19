package cost

import (
	"os"
	"strings"
	"testing"
)

func recordedSweep(t *testing.T) SweepResult {
	t.Helper()
	rows, err := RecordedAnswers()
	if err != nil {
		t.Fatalf("reading the recorded answers: %v", err)
	}
	return Sweep(rows, publishedPoint(t))
}

func TestTheSweepMakesNoModelCallAndTheCounterCanStillCountOne(t *testing.T) {
	result := recordedSweep(t)
	if result.ModelCalls != 0 {
		t.Fatalf("the sweep counted %d model calls over answers that are all recorded", result.ModelCalls)
	}
	live := Sweep([]AnswerRow{{Arm: "jev", Case: "probe", Label: Proceed, Verdict: Proceed, Live: true}}, publishedPoint(t))
	if live.ModelCalls != 1 {
		t.Fatalf("the counter read %d over one live row, so a zero above would prove nothing", live.ModelCalls)
	}
}

func TestTheSweepReproducesThePublishedTableAtThePublishedPoint(t *testing.T) {
	want := map[string]struct{ cases, correct, falseBlocks, caught int }{
		"jev":   {89, 68, 18, 3},
		"opus":  {89, 82, 2, 1},
		"fable": {51, 44, 1, 0},
	}
	for _, curve := range recordedSweep(t).Arms {
		expected, ok := want[curve.Arm]
		if !ok {
			t.Fatalf("the answers carry an arm %q the published report does not", curve.Arm)
		}
		at := curve.AtPublished
		if at.CorrectLow != at.CorrectHigh {
			t.Fatalf("%s: the published point is undetermined by %d cases, and it should be the one point every row was recorded at", curve.Arm, at.Undetermined)
		}
		got := struct{ cases, correct, falseBlocks, caught int }{curve.Cases, at.CorrectLow, at.FalseBlockLow, at.CaughtLow}
		if got != expected {
			t.Fatalf("%s: %+v, want %+v from bench/cost/report-2026-09-19.md", curve.Arm, got, expected)
		}
	}
}

func TestRaisingTheOperatingPointRemovesEveryFalseBlockJevAnswered(t *testing.T) {
	for _, curve := range recordedSweep(t).Arms {
		if curve.Arm != "jev" {
			continue
		}
		for _, point := range curve.Points {
			if point.ApprovalBlockAt != 0.80 {
				continue
			}
			if point.FalseBlockHigh != 0 {
				t.Fatalf("at 0.80 jev still answers block on up to %d cases the label calls proceed", point.FalseBlockHigh)
			}
			if point.CorrectLow < curve.AlwaysProceed {
				t.Fatalf("at 0.80 jev scores at least %d and the constant scores %d", point.CorrectLow, curve.AlwaysProceed)
			}
			return
		}
	}
	t.Fatal("the sweep produced no jev point at 0.80")
}

func TestTheCommittedSweepReportCarriesWhatTheSweepPrints(t *testing.T) {
	committed, err := os.ReadFile("sweep-2026-09-19.md")
	if err != nil {
		t.Fatalf("reading the committed sweep report: %v", err)
	}
	rendered := RenderSweep(recordedSweep(t))
	for _, block := range strings.Split(rendered, "\n\n") {
		if !strings.Contains(string(committed), strings.TrimSpace(block)) {
			t.Fatalf("the committed report is missing a block the sweep prints:\n%s", block)
		}
	}
}
