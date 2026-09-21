package sift

import (
	"testing"

	catalog "tofu/catalog"
	"tofu/internal/sift"
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
	r, err := sift.LoadShellRule(catalog.Files(), "shell_sift@1")
	if err != nil {
		t.Fatalf("load the shipped rule: %v", err)
	}
	return r
}

func TestTheShippedRuleIsShadowAndTheModelIsGivenTheOutputWhole(t *testing.T) {
	pol := shippedRule(t)
	if pol.Mode != sift.ModeShadow {
		t.Fatalf("catalog/tools/shell/rules/shell_sift@1.yaml is %s and nothing has been calibrated on shell output", pol.Mode)
	}
	wouldDrop := 0
	for _, row := range corpusRows(t) {
		planted, err := Plant(row, 0)
		if err != nil {
			t.Fatalf("Plant: %v", err)
		}
		marks := make([]sift.Mark, len(planted.Units))
		for i, unit := range planted.Units {
			marks[i] = sift.ShellCheap(unit)
			if !marks[i].Keep {
				wouldDrop++
			}
		}
		whole := sift.JoinUnits(planted.Units)
		if message := sift.Message(planted.Units, marks, pol.Mode); message != whole {
			t.Fatalf("%s: shadow gave the model %d bytes of a %d byte output", row.Session, len(message), len(whole))
		}
	}
	if wouldDrop == 0 {
		t.Fatal("the sieve would have removed nothing anywhere, so shadow proves nothing")
	}
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
