package turn

import (
	"path/filepath"
	"testing"
)

func recordedTurn(tb testing.TB) RecordedTurn {
	tb.Helper()
	recorded, err := ReadRecordedTurn(filepath.Join("..", "recall", "testdata", "recorded-turn.json"))
	if err != nil {
		tb.Fatalf("reading the recorded turn: %v", err)
	}
	return recorded
}

func BenchmarkRecordedTurnReplayedWithAndWithoutTheCallLayer(b *testing.B) {
	recorded := recordedTurn(b)
	var arms []ReplayArm
	for b.Loop() {
		arms = Replay(recorded)
	}
	b.Log("\n" + RenderReplay(recorded, arms))
}

func TestTheReplayRemovesOnlyCallsTheRecordProvesWereRemovable(t *testing.T) {
	recorded := recordedTurn(t)
	arms := Replay(recorded)
	t.Log("\n" + RenderReplay(recorded, arms))

	asRecorded, layered := arms[0], arms[1]
	if layered.Calls+layered.CacheHits+layered.Retries != asRecorded.Calls {
		t.Fatalf("the layered arm lost %d calls it never accounted for",
			asRecorded.Calls-layered.Calls-layered.CacheHits-layered.Retries)
	}
	if layered.Steps > asRecorded.Steps {
		t.Fatalf("the layered arm ran more steps than the record: %d against %d", layered.Steps, asRecorded.Steps)
	}
	if layered.Undecided == 0 {
		t.Fatal("every recorded failure was classed as repairable, which the record cannot support")
	}
}
