package recall

import (
	"path/filepath"
	"testing"
)

func realSessionsDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join("..", "..", ".tofu", "sessions")
	return dir
}

func TestNoRecordedSessionHasEverCrossedTheCeiling(t *testing.T) {
	reach, err := WalkCorpusReach(realSessionsDir(t))
	if err != nil {
		t.Fatalf("WalkCorpusReach: %v", err)
	}
	if len(reach.Measured) == 0 {
		t.Skip("no session on this machine carries occupancy: nothing to measure")
	}
	const ceiling = 250000
	if reach.Highest() >= ceiling {
		t.Fatalf("a recorded session peaked at %d, at or over the %d ceiling", reach.Highest(), ceiling)
	}
	t.Logf("%d sessions measured, highest %d (%.1f%% of the %d ceiling), median %d",
		len(reach.Measured), reach.Highest(), 100*float64(reach.Highest())/ceiling, ceiling, reach.Median())
}

func TestEverySessionThatCrossedTheCompactionTargetForkedRatherThanRewrote(t *testing.T) {
	reach, err := WalkCorpusReach(realSessionsDir(t))
	if err != nil {
		t.Fatalf("WalkCorpusReach: %v", err)
	}
	crossed := reach.Crossed()
	if len(crossed) != len(reach.Forks) {
		t.Fatalf("%d sessions crossed the compaction target recorded for them but %d forks are on record: a crossing that neither forked nor rewrote is unaccounted for",
			len(crossed), len(reach.Forks))
	}
	if reach.Compactions != 0 {
		t.Fatalf("an in place rewrite compaction fired %d times on real material; the report claims zero", reach.Compactions)
	}
	for _, fork := range reach.Forks {
		if fork.Kind != "continuation" {
			t.Fatalf("session %s forked as %q, not the continuation kind this test expects", fork.Session, fork.Kind)
		}
		t.Logf("%s step %d -> %s: %d tokens down to %d, carry named %d sources, %d refetched after",
			fork.Session, fork.Step, fork.Into, fork.TokensBefore, fork.TokensAfter, fork.KnownSources, fork.Refetches)
	}
}

func TestEverySkippedSessionNamesWhyItCouldNotBeMeasured(t *testing.T) {
	reach, err := WalkCorpusReach(realSessionsDir(t))
	if err != nil {
		t.Fatalf("WalkCorpusReach: %v", err)
	}
	if len(reach.Skipped) == 0 {
		t.Skip("no session on this machine failed to measure")
	}
	byReason := map[string]int{}
	for _, skip := range reach.Skipped {
		if skip.Reason == "" {
			t.Fatalf("session %s was skipped with no reason given", skip.ID)
		}
		byReason[skip.Reason]++
	}
	total := len(reach.Skipped) + len(reach.Measured)
	t.Logf("%d of %d recorded sessions could not be measured", len(reach.Skipped), total)
	for reason, count := range byReason {
		t.Logf("  %d skipped: %s", count, reason)
	}
}

func TestMeasuredPlusSkippedAccountsForEverySession(t *testing.T) {
	reach, err := WalkCorpusReach(realSessionsDir(t))
	if err != nil {
		t.Fatalf("WalkCorpusReach: %v", err)
	}
	if len(reach.Measured) == 0 && len(reach.Skipped) == 0 {
		t.Skip("no recorded session on this machine")
	}
	for i := 1; i < len(reach.Measured); i++ {
		if reach.Measured[i-1].Peak < reach.Measured[i].Peak {
			t.Fatalf("the measured list is not sorted highest first at index %d", i)
		}
	}
}
