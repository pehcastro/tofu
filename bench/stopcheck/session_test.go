package stopcheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/bench/corpus"
)

const (
	corpusDir         = "corpus"
	corpusFiles       = 19
	corpusSteps       = 79
	corpusLabelled    = 78
	corpusByteCeiling = 256 << 10
)

type labelAudit struct {
	steps     int
	empty     int
	onPurpose int
	gaps      []string
}

func auditLabels(turns []Turn) labelAudit {
	labels, declared := Labels(), Unlabelled()
	audit := labelAudit{}
	for _, turn := range turns {
		if len(turn.Steps) == 0 {
			audit.empty++
		}
		for _, step := range turn.Steps {
			audit.steps++
			key := stepKey(turn.ID, step.Index)
			why, named := declared[key]
			if _, labelled := labels[key]; labelled {
				if named {
					audit.gaps = append(audit.gaps, key+" carries a hand label and is also listed as deliberately unlabelled: "+why)
				}
				continue
			}
			if !named {
				audit.gaps = append(audit.gaps, key+" carries no hand label and is not listed in Unlabelled, so the battery would decide on it and count nothing")
				continue
			}
			audit.onPurpose++
			delete(declared, key)
		}
	}
	for key := range declared {
		audit.gaps = append(audit.gaps, key+" is listed as deliberately unlabelled but no such step is in the corpus")
	}
	return audit
}

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
	if !strings.Contains(skipped[0].Why, "no step and no wall clock") {
		t.Fatalf("the skip reason %q is not the shared reader's reason for a turn the old PascalCase schema wrote with no wall_clock_ms field it can bind to", skipped[0].Why)
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

func TestTheFrozenCorpusIsNineteenFilesAndStaysSmall(t *testing.T) {
	entries, err := os.ReadDir(corpusDir)
	if err != nil {
		t.Fatalf("ReadDir %s: %v", corpusDir, err)
	}
	files, bytes := 0, int64(0)
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			t.Fatalf("%s: %v", entry.Name(), err)
		}
		files++
		bytes += info.Size()
	}
	if files != corpusFiles {
		t.Errorf("%s holds %d turn files, want the %d that were labelled by hand. a turn enters this set by being copied here on purpose, and the counts move with it", corpusDir, files, corpusFiles)
	}
	if bytes > corpusByteCeiling {
		t.Errorf("the frozen corpus is %d bytes, over the %d the repository is willing to carry for it", bytes, corpusByteCeiling)
	}
	t.Logf("%s: %d files, %d bytes", corpusDir, files, bytes)
}

func TestEveryStepInTheFrozenCorpusCarriesAHandLabel(t *testing.T) {
	turns, skipped, err := ReadSessions(corpusDir)
	if err != nil {
		t.Fatalf("ReadSessions: %v", err)
	}
	audit := auditLabels(turns)
	for _, gap := range audit.gaps {
		t.Error(gap)
	}
	labelled := audit.steps - audit.onPurpose
	if audit.steps != corpusSteps || labelled != corpusLabelled {
		t.Errorf("the corpus holds %d steps of which %d are labelled, want %d and %d", audit.steps, labelled, corpusSteps, corpusLabelled)
	}
	t.Logf("%s holds %d readable turns, %d of them with no step, %d skipped files and %d steps in all. hand labels: %d. unlabelled on purpose: %d",
		corpusDir, len(turns), audit.empty, len(skipped), audit.steps, labelled, audit.onPurpose)
	for _, s := range skipped {
		t.Logf("skipped %s: %s", s.File, s.Why)
	}
}

func TestATurnInTheCorpusWithNoHandLabelStillFails(t *testing.T) {
	turns, _, err := ReadSessions(corpusDir)
	if err != nil {
		t.Fatalf("ReadSessions: %v", err)
	}
	unlabelled := Turn{ID: "turn-18d6a74abeb9c3ec", Task: "a turn recorded after the corpus was frozen", Steps: []Step{{Index: 1}}}
	audit := auditLabels(append(turns, unlabelled))
	if len(audit.gaps) != 1 {
		t.Fatalf("the audit found %d gaps %v, want only the turn with no label", len(audit.gaps), audit.gaps)
	}
	if !strings.HasPrefix(audit.gaps[0], "turn-18d6a74abeb9c3ec#1 carries no hand label and is not listed in Unlabelled") {
		t.Fatalf("the audit said %q, not the message the frozen corpus test would print", audit.gaps[0])
	}
	t.Logf("%s", audit.gaps[0])
}

func TestNoFrozenTurnCarriesThisMachinesIdentity(t *testing.T) {
	entries, err := os.ReadDir(corpusDir)
	if err != nil {
		t.Fatalf("ReadDir %s: %v", corpusDir, err)
	}
	for _, entry := range entries {
		raw, err := os.ReadFile(filepath.Join(corpusDir, entry.Name()))
		if err != nil {
			t.Fatalf("%s: %v", entry.Name(), err)
		}
		if clean := corpus.Scrub(string(raw)); clean != string(raw) {
			t.Errorf("%s is not a fixed point of the scrub, so it still carries a path or a name from the machine that recorded it", entry.Name())
		}
	}
	t.Logf("every file in %s equals its own scrub", corpusDir)
}
