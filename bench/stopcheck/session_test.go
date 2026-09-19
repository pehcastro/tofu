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
	labels, declared := Labels(), Unlabelled()
	steps, empty, onPurpose := 0, 0, 0
	for _, turn := range turns {
		if len(turn.Steps) == 0 {
			empty++
		}
		for _, step := range turn.Steps {
			steps++
			key := stepKey(turn.ID, step.Index)
			why, named := declared[key]
			if _, labelled := labels[key]; labelled {
				if named {
					t.Errorf("%s carries a hand label and is also listed as deliberately unlabelled: %s", key, why)
				}
				continue
			}
			if !named {
				t.Errorf("%s carries no hand label and is not listed in Unlabelled, so the battery would decide on it and count nothing", key)
				continue
			}
			onPurpose++
			delete(declared, key)
			t.Logf("unlabelled on purpose: %s: %s", key, why)
		}
	}
	for key := range declared {
		t.Errorf("%s is listed as deliberately unlabelled but no such step is in the corpus", key)
	}
	t.Logf("%s holds %d readable turns, %d of them with no step, %d skipped files and %d steps in all. hand labels: %d. unlabelled on purpose: %d",
		sessionsDir, len(turns), empty, len(skipped), steps, steps-onPurpose, onPurpose)
	for _, s := range skipped {
		t.Logf("skipped %s: %s", s.File, s.Why)
	}
	if steps < 40 {
		t.Fatalf("the recorded corpus holds %d steps and BOJI-079 asks for at least forty", steps)
	}
}
