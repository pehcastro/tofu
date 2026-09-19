package cost

import (
	"os"
	"strings"
	"testing"
)

func recordedRescore(t *testing.T) RescoreResult {
	t.Helper()
	rows, err := RecordedAnswers()
	if err != nil {
		t.Fatalf("reading the recorded answers: %v", err)
	}
	pol, resolution := shippedGate(t)
	_, heldOut := halves(t)
	deterministic := []ArmResult{runRegex(heldOut), runAlwaysProceed(heldOut)}
	rows = append(rows, answerRows(deterministic, pol, deterministicArmSource)...)
	result, err := Rescore(rows, pol, resolution)
	if err != nil {
		t.Fatalf("re-scoring: %v", err)
	}
	return result
}

func TestTheCommittedRescoreReportCarriesWhatTheRescorePrints(t *testing.T) {
	committed, err := os.ReadFile("rescore-2026-09-19.md")
	if err != nil {
		t.Fatalf("reading the committed re-score report: %v", err)
	}
	rendered := RenderRescore(recordedRescore(t))
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
		"| Arm | Case | Answer | Label | Labelled by | user_requested | approval | The call |",
	} {
		if !strings.Contains(string(published), phrase) {
			t.Fatalf("report-2026-09-19.md no longer carries %q, so it was edited", phrase)
		}
	}
}
