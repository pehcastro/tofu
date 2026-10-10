package search

import (
	"errors"
	"os"
	"testing"
	"time"

	tool "tofu/internal/search"
	"tofu/internal/sys"
)

const treeRoot = "../.."

var reportGeneratedAt = time.Date(2026, time.September, 23, 8, 38, 15, 0, time.FixedZone("-03", -3*60*60))

func loadOrSkip(t *testing.T) Corpus {
	t.Helper()
	sessionsDir := sys.RecordedStateDir("sessions")
	if _, err := os.Stat(sessionsDir); err != nil {
		t.Skipf("skipped: %s is not on this machine, so there is no recorded search to replay", sessionsDir)
	}
	loaded, err := Load(sessionsDir, reportGeneratedAt)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d turns recorded after %s, not read", loaded.Later, reportGeneratedAt.Format(time.RFC3339))
	if len(loaded.Rows) == 0 {
		t.Skipf("skipped: %d recorded turns hold no search call", loaded.Turns)
	}
	return loaded
}

func TestHowOftenASinglePatternFindReturnsNothingWhereACandidateAnswers(t *testing.T) {
	loaded := loadOrSkip(t)

	for _, skipped := range loaded.Skipped {
		t.Logf("the walk skipped %s", skipped)
	}
	replayed, gone, literalEmpty, rescued, absent := 0, 0, 0, 0, 0
	for _, row := range loaded.Rows {
		outcome, err := Replay(treeRoot, row)
		if errors.Is(err, ErrPathGone) {
			gone++
			t.Logf("skipped %s %q under %q: the recorded path is not in the tree today", row.Turn, row.Pattern, row.Path)
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		replayed++
		switch {
		case outcome.Answered == tool.Literal:
			continue
		case outcome.Answered != tool.NoCandidate:
			literalEmpty++
			rescued++
			t.Logf("rescued %q under %q: the literal found 0 lines and the %s form found %d in %d files",
				row.Pattern, row.Path, outcome.Answered, outcome.Attempt(outcome.Answered).Lines, outcome.Attempt(outcome.Answered).Files)
		default:
			literalEmpty++
			absent++
			t.Logf("absent %q under %q: no form finds it", row.Pattern, row.Path)
		}
	}

	t.Logf("corpus: %d recorded turns under %s, %d recorded search calls, %d replayed, %d skipped because the recorded path is gone, %d turns skipped by the walk",
		loaded.Turns, loaded.Dir, len(loaded.Rows), replayed, gone, len(loaded.Skipped))
	t.Logf("single pattern Find returned nothing on %d of %d replayed searches", literalEmpty, replayed)
	t.Logf("a candidate form answered %d of those %d, and %d were a real absence", rescued, literalEmpty, absent)
	if replayed == 0 {
		t.Skip("skipped: every recorded path is gone from the tree, so nothing could be replayed")
	}
	if rescued+absent != literalEmpty {
		t.Fatalf("the counts do not add up: %d rescued plus %d absent is not %d", rescued, absent, literalEmpty)
	}
}

func TestEveryReplayedRowNamesTheFormThatAnsweredIt(t *testing.T) {
	loaded := loadOrSkip(t)

	known := map[tool.Candidate]bool{
		tool.Literal:         true,
		tool.WordBoundary:    true,
		tool.CaseInsensitive: true,
		tool.NoCandidate:     true,
	}
	for _, row := range loaded.Rows {
		outcome, err := Replay(treeRoot, row)
		if errors.Is(err, ErrPathGone) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if !known[outcome.Answered] {
			t.Fatalf("%q under %q was answered by %q, which is not a form that can answer", row.Pattern, row.Path, outcome.Answered)
		}
		if len(outcome.Attempts) != 4 {
			t.Fatalf("%q under %q reports %d candidate forms, want the four the design names", row.Pattern, row.Path, len(outcome.Attempts))
		}
	}
}
