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
		arms, _ = Replay(recorded)
	}
	b.Log("\n" + RenderReplay(recorded, arms))
}

func TestTheReplayRemovesOnlyCallsTheRecordProvesWereRemovable(t *testing.T) {
	recorded := recordedTurn(t)
	arms, _ := Replay(recorded)
	t.Log("\n" + RenderReplay(recorded, arms))

	asRecorded, layered := arms[0], arms[1]
	if layered.Calls+layered.CacheHits+layered.Retries != asRecorded.Calls {
		t.Fatalf("the layered arm lost %d calls it never accounted for",
			asRecorded.Calls-layered.Calls-layered.CacheHits-layered.Retries)
	}
	if layered.Steps > asRecorded.Steps {
		t.Fatalf("the layered arm ran more steps than the record: %d against %d", layered.Steps, asRecorded.Steps)
	}
	if layered.Repaired == 0 || layered.Refusals == 0 {
		t.Fatalf("this fixture carries both a repairable and a refused edit failure, and the split must keep both: repaired %d, refused %d", layered.Repaired, layered.Refusals)
	}
	if asRecorded.Refusals != layered.Refusals {
		t.Fatalf("refusals are a property of the call itself, not of the arm: as recorded %d, layered %d", asRecorded.Refusals, layered.Refusals)
	}
	if layered.BytesReturned >= asRecorded.BytesReturned {
		t.Fatalf("the layered arm skips calls the memo already answered, so it must return fewer bytes: as recorded %d, layered %d", asRecorded.BytesReturned, layered.BytesReturned)
	}
	if asRecorded.AnswersChanged != 0 {
		t.Fatalf("the recorded arm is the baseline and cannot differ from itself: got %d", asRecorded.AnswersChanged)
	}
	if layered.AnswersChanged != layered.CacheHits+layered.Repaired {
		t.Fatalf("answers change only on a cache hit or a repair: got %d, want %d", layered.AnswersChanged, layered.CacheHits+layered.Repaired)
	}
}
