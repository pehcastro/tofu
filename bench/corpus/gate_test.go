package corpus

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"testing"

	"tofu/internal/judge/state"
)

func TestGateCorpusHoldsAtLeastOneHundredAndTwentyRecordedCases(t *testing.T) {
	records, err := GateRecords()
	if err != nil {
		t.Fatalf("loading the corpus: %v", err)
	}
	recorded := 0
	for _, record := range records {
		if record.Recorded {
			recorded++
		}
	}
	if recorded < 120 {
		t.Fatalf("recorded cases = %d, the ticket asks for at least 120", recorded)
	}
	t.Logf("%d cases, %d of them recorded from a run", len(records), recorded)
}

func TestEveryGateCaseCarriesProvenanceAndTheShapeTheCurrentBuilderProduces(t *testing.T) {
	records, err := GateRecords()
	if err != nil {
		t.Fatalf("loading the corpus: %v", err)
	}
	want := state.ToolGateVersion()
	seen := map[string]bool{}
	for _, record := range records {
		if seen[record.ID] {
			t.Fatalf("%s appears twice", record.ID)
		}
		seen[record.ID] = true
		if record.Origin == "" || record.RecordedAt == "" || record.LabelNote == "" {
			t.Errorf("%s: origin %q, recorded_at %q, label_note %q, all three are provenance and none may be empty", record.ID, record.Origin, record.RecordedAt, record.LabelNote)
		}
		if record.StateShape != want {
			t.Errorf("%s: state_shape %q, the current builder produces %q", record.ID, record.StateShape, want)
		}
	}
}

func TestTheSplitIsAPureFunctionOfTheIdAndTheLabel(t *testing.T) {
	records, err := GateRecords()
	if err != nil {
		t.Fatalf("loading the corpus: %v", err)
	}
	split, err := GateSplit()
	if err != nil {
		t.Fatalf("loading the split: %v", err)
	}
	var train, heldout []string
	for _, label := range []Label{Block, Proceed} {
		var ids []string
		for _, record := range records {
			if record.Label == label {
				ids = append(ids, record.ID)
			}
		}
		sort.Slice(ids, func(i, j int) bool { return digestOf(ids[i]) < digestOf(ids[j]) })
		for rank, id := range ids {
			if rank%2 == 0 {
				train = append(train, id)
				continue
			}
			heldout = append(heldout, id)
		}
	}
	sort.Strings(train)
	sort.Strings(heldout)
	if strings.Join(train, ",") != strings.Join(split.Train, ",") {
		t.Errorf("the recorded training half is not what the recorded method produces")
	}
	if strings.Join(heldout, ",") != strings.Join(split.Heldout, ",") {
		t.Errorf("the recorded held-out half is not what the recorded method produces")
	}
	if got := digestOf(strings.Join(split.Heldout, "\n")); got != split.HeldoutDigest {
		t.Errorf("heldout_digest = %s, the held-out list hashes to %s", split.HeldoutDigest, got)
	}
	if len(split.Train)+len(split.Heldout) != len(records) {
		t.Errorf("the two halves hold %d ids for %d cases", len(split.Train)+len(split.Heldout), len(records))
	}
}

func TestTheCorpusIsAsciiBecauseTheRepositoryRefusesAnEmDash(t *testing.T) {
	for _, name := range []string{"gate/cases.jsonl", "gate/cases-whole.jsonl"} {
		raw, err := gateFiles.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for i, b := range raw {
			if b > 127 {
				t.Fatalf("%s byte %d is 0x%x, the corpus must be ascii", name, i, b)
			}
		}
	}
}

func TestEveryCutCommandWasRecoveredWholeBesideTheCaseThatCarriesItCut(t *testing.T) {
	records, err := GateRecords()
	if err != nil {
		t.Fatalf("loading the corpus: %v", err)
	}
	whole, err := GateWholeCommandRecords()
	if err != nil {
		t.Fatalf("loading the recovered commands: %v", err)
	}
	cut := CutCommands(records)
	if len(cut) != 33 || len(records) != 178 {
		t.Fatalf("%d of %d cases carry a cut command, the report states 33 of 178", len(cut), len(records))
	}
	split, err := GateSplit()
	if err != nil {
		t.Fatalf("loading the split: %v", err)
	}
	heldOutCut := 0
	for _, id := range split.Heldout {
		if cut[id] {
			heldOutCut++
		}
	}
	if heldOutCut != 14 {
		t.Errorf("%d of the held-out cases carry a cut command, the report states 14", heldOutCut)
	}
	t.Logf("%d of %d cases carry a command cut at recording time, %d of the %d held out, %d recovered whole", len(cut), len(records), heldOutCut, len(split.Heldout), len(whole))
	if len(whole) != len(cut) {
		t.Fatalf("%d cases were recovered whole for %d cut commands", len(whole), len(cut))
	}
	recorded := map[string]Record{}
	for _, record := range records {
		recorded[record.ID] = record
	}
	for _, record := range whole {
		original, known := recorded[record.ID]
		switch {
		case !known:
			t.Errorf("%s was recovered whole and is not in the corpus", record.ID)
		case !cut[record.ID]:
			t.Errorf("%s was recovered whole and its recorded command was never cut", record.ID)
		case strings.Contains(record.Command(), TruncationMark):
			t.Errorf("%s still carries %q after recovery", record.ID, TruncationMark)
		case len(record.Command()) <= len(original.Command()):
			t.Errorf("%s recovered to %d characters from %d", record.ID, len(record.Command()), len(original.Command()))
		case record.Label != original.Label || record.LabelBy != original.LabelBy || record.LabelNote != original.LabelNote:
			t.Errorf("%s changed its label, and this ticket changes no label", record.ID)
		}
	}
}

func digestOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
