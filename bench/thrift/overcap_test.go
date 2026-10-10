package thrift

import (
	"testing"
	"time"

	"tofu/bench/corpus"
)

var overCapReportGeneratedAt = time.Date(2026, time.September, 23, 9, 25, 4, 0, time.FixedZone("-03", -3*60*60))

func TestOverCapTargetsMatchTheNineIdentifiedInSectionFour(t *testing.T) {
	live, err := corpus.WalkSessions(sessionsDir())
	if err != nil {
		t.Fatalf("WalkSessions(%q): %v", sessionsDir(), err)
	}
	walked := live.RecordedBy(overCapReportGeneratedAt)
	t.Logf("%d turns recorded after %s, not read", walked.Later, overCapReportGeneratedAt.Format(time.RFC3339))
	targets, idSkips := OverCapReadAndSearchTargets(artifactsDir(), walked.Turns)
	if len(targets)+len(idSkips) != 9 {
		t.Fatalf("%d over-cap read or search artifacts found, want 9 as report-2026-09-23.md section 4 counts", len(targets)+len(idSkips))
	}
	for i := 1; i < len(targets); i++ {
		if targets[i].Paragraphs < targets[i-1].Paragraphs {
			t.Fatalf("targets not sorted ascending by paragraph count at index %d", i)
		}
	}
}

func TestOverlapLenOnKnownRanges(t *testing.T) {
	cases := []struct {
		lo, hi, lo2, hi2, want int
	}{
		{0, 10, 5, 15, 5},
		{0, 10, 10, 20, 0},
		{0, 10, 20, 30, 0},
		{5, 15, 0, 20, 10},
	}
	for _, c := range cases {
		if got := overlapLen(c.lo, c.hi, c.lo2, c.hi2); got != c.want {
			t.Fatalf("overlapLen(%d,%d,%d,%d) = %d, want %d", c.lo, c.hi, c.lo2, c.hi2, got, c.want)
		}
	}
}
