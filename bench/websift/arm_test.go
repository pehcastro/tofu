package websift

import (
	"fmt"
	"strings"
	"testing"

	catalogpolicy "tofu/catalog/policy"
	"tofu/internal/sift"
)

func tally(t *testing.T, arm string, readings []Reading) {
	t.Helper()
	kept, before, after := 0, 0, 0
	var lost []string
	for i, reading := range readings {
		before, after = before+reading.Before, after+reading.After
		if reading.NeedleKept {
			kept++
			continue
		}
		lost = append(lost, fmt.Sprintf("row %d unit %d", i, reading.NeedleAt))
	}
	t.Logf("%s: %d of %d needles kept, %d bytes to %d, %.1f%% saved, lost at %s",
		arm, kept, len(readings), before, after, 100*float64(before-after)/float64(before), strings.Join(lost, ", "))
}

const corpusFloor = 20

func corpusRows(t *testing.T) []Row {
	t.Helper()
	rows, err := ReadCorpus()
	if err != nil {
		t.Fatalf("ReadCorpus: %v", err)
	}
	if len(rows) < corpusFloor {
		t.Fatalf("the corpus carries %d fetched pages and the ticket asks for %d", len(rows), corpusFloor)
	}
	return rows
}

func byGroup(rows []Row, group string) []Row {
	var out []Row
	for _, row := range rows {
		if row.Group == group {
			out = append(out, row)
		}
	}
	return out
}

func shippedPolicy(t *testing.T) sift.PagePolicy {
	t.Helper()
	pol, err := sift.LoadPagePolicy(catalogpolicy.Files(), "page_sift@1.yaml")
	if err != nil {
		t.Fatalf("load the shipped policy: %v", err)
	}
	return pol
}

func TestTheShippedPolicyIsShadowAndTheModelIsGivenThePageWhole(t *testing.T) {
	pol := shippedPolicy(t)
	if pol.Mode != sift.ModeShadow {
		t.Fatalf("catalog/policy/page_sift@1.yaml is %s and nothing has been calibrated on fetched pages", pol.Mode)
	}
	wouldDrop := 0
	for i, row := range corpusRows(t) {
		planted, err := Plant(row, i)
		if err != nil {
			t.Fatalf("Plant: %v", err)
		}
		marks := make([]sift.Mark, len(planted.Units))
		for j, unit := range planted.Units {
			marks[j] = sift.PageCheap(unit)
			if !marks[j].Keep {
				wouldDrop++
			}
		}
		whole := sift.JoinPageUnits(planted.Units)
		if message := sift.PageMessage(planted.Units, marks, pol.Mode); message != whole {
			t.Fatalf("%s: shadow gave the model %d bytes of a %d byte page", row.Source, len(message), len(whole))
		}
	}
	if wouldDrop == 0 {
		t.Fatal("the readability arm would have removed nothing anywhere, so shadow proves nothing")
	}
}

func TestTheFreeArmsOverEveryFetchedPage(t *testing.T) {
	rows := corpusRows(t)
	for _, group := range []string{GroupFocused, GroupReference} {
		var readability, keepAll []Reading
		for i, row := range byGroup(rows, group) {
			planted, err := Plant(row, i)
			if err != nil {
				t.Fatalf("Plant: %v", err)
			}
			readability = append(readability, Free(row, planted))
			keepAll = append(keepAll, KeepEverything(row, planted))
			t.Log(readability[len(readability)-1].Line())
		}
		tally(t, group+" readability arm", readability)
		tally(t, group+" keep everything arm", keepAll)
	}
}

func TestEveryHeldUnitSurvivesTheFreeArmOnRealPages(t *testing.T) {
	for i, row := range corpusRows(t) {
		planted, err := Plant(row, i)
		if err != nil {
			t.Fatalf("Plant: %v", err)
		}
		for _, unit := range planted.Units {
			if unit.Held == sift.NotHeld {
				continue
			}
			if mark := sift.PageCheap(unit); !mark.Keep {
				t.Fatalf("%s: unit %d is held as %q and the free arm dropped it: %s", row.Source, unit.Index, unit.Held, mark.Reason)
			}
		}
	}
}
