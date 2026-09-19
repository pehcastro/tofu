package corpus

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"testing"

	"boji/internal/judge/state"
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
	raw, err := gateFiles.ReadFile("gate/cases.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	for i, b := range raw {
		if b > 127 {
			t.Fatalf("byte %d is 0x%x, the corpus must be ascii", i, b)
		}
	}
}

func digestOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
