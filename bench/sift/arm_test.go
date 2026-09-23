package sift

import (
	"strings"
	"testing"

	"tofu/internal/sift"
	library "tofu/library"
)

const corpusFloor = 20

func corpusRows(t *testing.T) []Row {
	t.Helper()
	rows, err := ReadCorpus()
	if err != nil {
		t.Fatalf("ReadCorpus: %v", err)
	}
	if len(rows) < corpusFloor {
		t.Fatalf("the corpus carries %d captured outputs and the ticket asks for %d", len(rows), corpusFloor)
	}
	return rows
}

func shippedRule(t *testing.T) sift.ShellRule {
	t.Helper()
	r, err := sift.LoadShellRule(library.Files(), "shell_sift@1")
	if err != nil {
		t.Fatalf("load the shipped rule: %v", err)
	}
	return r
}

func TestTheShippedRuleIsEnforcedAndTheModelIsGivenTheOutputCut(t *testing.T) {
	pol := shippedRule(t)
	if pol.Mode != sift.ModeEnforced {
		t.Fatalf("library/tools/shell/rules/shell_sift@1.yaml is %s and the model is still given every shell result whole", pol.Mode)
	}
	dropped, cutRows, wholeBytes, sentBytes := 0, 0, 0, 0
	for _, row := range corpusRows(t) {
		planted, err := Plant(row, 0)
		if err != nil {
			t.Fatalf("Plant: %v", err)
		}
		marks := make([]sift.Mark, len(planted.Units))
		rowDropped := 0
		for i, unit := range planted.Units {
			marks[i] = sift.ShellCheap(unit)
			if !marks[i].Keep {
				rowDropped++
			}
		}
		whole := sift.JoinUnits(planted.Units)
		message := sift.Message(planted.Units, marks, pol.Mode)
		dropped += rowDropped
		wholeBytes += len(whole)
		sentBytes += len(message)
		if rowDropped == 0 {
			if message != whole {
				t.Fatalf("%s: no unit was dropped and the model was given something other than the output", row.Session)
			}
			continue
		}
		cutRows++
		if !strings.Contains(message, "[sift:") {
			t.Fatalf("%s: %d units were dropped and the message says nothing about it", row.Session, rowDropped)
		}
	}
	if dropped == 0 {
		t.Fatal("the sieve removed nothing anywhere, so enforcement proves nothing")
	}
	if sentBytes >= wholeBytes {
		t.Fatalf("the corpus sent %d bytes of %d, so enforcement saved nothing", sentBytes, wholeBytes)
	}
	t.Logf("%d rows cut, %d units dropped, %d bytes of %d sent", cutRows, dropped, sentBytes, wholeBytes)
}

func TestTheFreeArmOverEveryCapturedOutput(t *testing.T) {
	var readings []Reading
	for i, row := range corpusRows(t) {
		planted, err := Plant(row, i)
		if err != nil {
			t.Fatalf("Plant: %v", err)
		}
		readings = append(readings, Free(row, planted))
		t.Log(readings[i].Line())
	}
	tally(t, "free arm", readings)
}

func TestEveryHeldChunkSurvivesTheFreeArmOnRealOutput(t *testing.T) {
	for i, row := range corpusRows(t) {
		planted, err := Plant(row, i)
		if err != nil {
			t.Fatalf("Plant: %v", err)
		}
		for _, unit := range planted.Units {
			if unit.Held == sift.NotHeld {
				continue
			}
			if mark := sift.ShellCheap(unit); !mark.Keep {
				t.Fatalf("%s: unit %d is held as %q and the free arm dropped it: %s", row.Session, unit.Index, unit.Held, mark.Reason)
			}
		}
	}
}
