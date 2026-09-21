package cost

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func rescoreWith(t *testing.T, extra []AnswerRow) RescoreResult {
	t.Helper()
	rows, err := RecordedAnswers()
	if err != nil {
		t.Fatalf("reading the recorded answers: %v", err)
	}
	pol, resolution := shippedGate(t)
	_, heldOut := halves(t)
	deterministic := []ArmResult{runRegex(heldOut), runAlwaysProceed(heldOut)}
	rows = append(rows, answerRows(deterministic, pol, deterministicArmSource)...)
	result, err := Rescore(append(rows, extra...), pol, resolution)
	if err != nil {
		t.Fatalf("re-scoring: %v", err)
	}
	return result
}

func recordedRescore(t *testing.T) RescoreResult {
	t.Helper()
	return rescoreWith(t, nil)
}

func calibratedRescore(t *testing.T) RescoreResult {
	t.Helper()
	calibrated, err := CalibrationRescoreRows()
	if err != nil {
		t.Fatalf("reading the calibration answers: %v", err)
	}
	return rescoreWith(t, calibrated)
}

func TestTheDeadBandIsMeasuredFromTheCutsTheGateCompares(t *testing.T) {
	pol, _ := shippedGate(t)
	rendered := RenderRescore(calibratedRescore(t))
	_, section, found := strings.Cut(rendered, "## Rows whose answer sat on the cut")
	if !found {
		t.Fatal("the rendered re-score carries no dead band section")
	}
	for _, want := range []string{
		fmt.Sprintf("user_requested %.2f", pol.Thresholds.UserRequestedRelaxAt),
		fmt.Sprintf("approval %.2f", pol.Thresholds.ApprovalRelaxAt),
		fmt.Sprintf("risk %.2f", pol.Thresholds.RiskAskAt),
	} {
		if !strings.Contains(section, want) {
			t.Errorf("the dead band section does not name %q, so it measures distance from a cut gate.Decide never compares against", want)
		}
	}
}

func TestTheRescoreActuallyRescoresTheRowsBOJI122Recorded(t *testing.T) {
	result := calibratedRescore(t)
	total := 0
	for _, arm := range result.Arms {
		total += arm.Rescored
	}
	if total == 0 {
		t.Fatalf("the re-score re-scored %d rows across %d arms, and bench/cost/calibration carries 178 rows with all four answers", total, len(result.Arms))
	}
	t.Logf("re-scored %d rows across %d arms", total, len(result.Arms))
}

func TestTheRescoredGateIsBehindAlwaysProceedOnTheWholeCorpus(t *testing.T) {
	rows, err := CalibrationRescoreRows()
	if err != nil {
		t.Fatalf("reading the calibration answers: %v", err)
	}
	blocks := 0
	for _, row := range rows {
		if row.Label == Block {
			blocks++
		}
	}
	gate, alwaysProceed := 0, len(rows)-blocks
	for _, arm := range calibratedRescore(t).Arms {
		if arm.Arm == CalibrationArm {
			gate = arm.CorrectRescored
		}
	}
	t.Logf("over %d rows with %d blocks: the gate agrees with %d labels, always-proceed with %d", len(rows), blocks, gate, alwaysProceed)
	if gate > alwaysProceed {
		t.Fatalf("the gate scores %d and always-proceed %d, so rescore-2026-09-19.md is wrong to say the gate is behind", gate, alwaysProceed)
	}
}

func TestTheCommittedRescoreReportCarriesWhatTheRescorePrints(t *testing.T) {
	committed, err := os.ReadFile("rescore-2026-09-19.md")
	if err != nil {
		t.Fatalf("reading the committed re-score report: %v", err)
	}
	rendered := RenderRescore(calibratedRescore(t))
	for _, block := range strings.Split(rendered, "\n\n") {
		if !strings.Contains(string(committed), strings.TrimSpace(block)) {
			t.Fatalf("the committed report is missing a block the re-score prints:\n%s", block)
		}
	}
}

func TestThePublishedReportIsUntouchedByThisTicket(t *testing.T) {
	published, err := os.ReadFile("report-2026-09-19.md")
	if err != nil {
		t.Fatalf("reading the published report: %v", err)
	}
	for _, phrase := range []string{
		"user_requested and approval are the two answers the decision reads",
		"| Arm | Case | Answer | Label | Labelled by | user_requested | approval |",
	} {
		if !strings.Contains(string(published), phrase) {
			t.Fatalf("report-2026-09-19.md no longer carries %q, so it was edited", phrase)
		}
	}
}
