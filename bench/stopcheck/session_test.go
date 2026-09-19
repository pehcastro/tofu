package stopcheck

import (
	"path/filepath"
	"strings"
	"testing"
)

const sessionsDir = "../../.boji/sessions"

func TestReadSessionsSkipsATurnWrittenBeforeTheOutcomeWasAString(t *testing.T) {
	turns, skipped, err := ReadSessions(filepath.Join("testdata", "sessions"))
	if err != nil {
		t.Fatalf("ReadSessions: %v", err)
	}
	if len(turns) != 1 || turns[0].ID != "turn-modern" {
		t.Fatalf("read %d turns %v, want only turn-modern", len(turns), turns)
	}
	if len(skipped) != 1 || skipped[0].File != "turn-legacy.json" {
		t.Fatalf("skipped %v, want turn-legacy.json", skipped)
	}
	if !strings.Contains(skipped[0].Why, "older") {
		t.Fatalf("the skip reason %q does not say why the file was skipped", skipped[0].Why)
	}
	t.Logf("skipped: %s: %s", skipped[0].File, skipped[0].Why)
}

func TestReadSessionsKeepsCommandsFailuresAndGateIDs(t *testing.T) {
	turns, _, err := ReadSessions(filepath.Join("testdata", "sessions"))
	if err != nil {
		t.Fatalf("ReadSessions: %v", err)
	}
	step := turns[0].Steps[0]
	if len(step.Calls) != 1 {
		t.Fatalf("step 1 carries %d calls, want 1", len(step.Calls))
	}
	call := step.Calls[0]
	if call.Command != "ls -la" || !call.Failed || call.GateDecision != "2026-09-19-abc" {
		t.Fatalf("call = %+v, want the command, the failure and the gate decision id kept", call)
	}
}

func TestEveryReadableRecordedStepCarriesAHandLabel(t *testing.T) {
	turns, skipped, err := ReadSessions(sessionsDir)
	if err != nil {
		t.Fatalf("ReadSessions: %v", err)
	}
	labels := Labels()
	steps, empty := 0, 0
	var unlabelled []string
	for _, turn := range turns {
		if len(turn.Steps) == 0 {
			empty++
		}
		for _, step := range turn.Steps {
			steps++
			if _, ok := labels[stepKey(turn.ID, step.Index)]; !ok {
				unlabelled = append(unlabelled, stepKey(turn.ID, step.Index))
			}
		}
	}
	t.Logf("%s holds %d readable turns, %d of them with no step, %d skipped files and %d steps in all",
		sessionsDir, len(turns), empty, len(skipped), steps)
	for _, s := range skipped {
		t.Logf("skipped %s: %s", s.File, s.Why)
	}
	if steps < 40 {
		t.Fatalf("the recorded corpus holds %d steps and BOJI-079 asks for at least forty", steps)
	}
	if len(unlabelled) > 0 {
		t.Fatalf("%d recorded steps carry no hand label, so the battery would decide on them and count nothing: %v", len(unlabelled), unlabelled)
	}
}
