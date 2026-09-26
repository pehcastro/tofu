package thrift

import (
	"strings"
	"testing"

	"tofu/bench/corpus"
	"tofu/internal/sys"
)

func sessionsDir() string { return sys.RecordedStateDir("sessions") }

func artifactsDir() string { return sys.RecordedStateDir("artifacts") }

const turnsGainedFromSplitRestarts = 7

func TestEveryCorpusEntryIsEitherASessionOrANamedSkip(t *testing.T) {
	walked, err := corpus.WalkSessions(sessionsDir())
	if err != nil {
		t.Fatalf("WalkSessions(%q): %v", sessionsDir(), err)
	}
	if len(walked.Turns) == 0 {
		t.Fatal("no session was read: the path is wrong or the corpus is empty")
	}
	if len(walked.Turns)+len(walked.Skipped) != walked.EntryCount+turnsGainedFromSplitRestarts {
		t.Fatalf("%d entries, %d sessions and %d skips: an entry went unaccounted for beyond the %d extra turns a restarted session directory now splits into", walked.EntryCount, len(walked.Turns), len(walked.Skipped), turnsGainedFromSplitRestarts)
	}
	for _, skip := range walked.Skipped {
		if skip.Reason == "" {
			t.Fatalf("%s was skipped with no reason", skip.Path)
		}
	}
}

func TestSpreadOverAKnownSeries(t *testing.T) {
	spread := Measure([]int64{0, 1, 2, 3, 4, 5, 6, 7, 8, 100})
	if spread.Count != 10 || spread.Sum != 136 || spread.Zeros != 1 {
		t.Fatalf("count %d, sum %d, zeros %d", spread.Count, spread.Sum, spread.Zeros)
	}
	if spread.Median != 4 || spread.Worst != 100 || spread.P90 != 8 {
		t.Fatalf("median %d, p90 %d, worst %d", spread.Median, spread.P90, spread.Worst)
	}
}

func TestHistogramPlacesEveryValueInExactlyOneBand(t *testing.T) {
	values := []int64{0, 1, 2, 5, 10, 20, 50, 100, 250, 500, 4000}
	counted := 0
	for _, bucket := range Histogram(values) {
		counted += bucket.Count
	}
	if counted != len(values) {
		t.Fatalf("%d values counted out of %d", counted, len(values))
	}
}

func TestBlankDuplicateAndTrailingBytesNeverOverlap(t *testing.T) {
	content := "alpha  \n\nalpha\nbeta\n\nalpha   \n"
	one := measureOne(content, 1<<20)
	if one.BlankBytes != 2 {
		t.Fatalf("blank bytes %d, want 2", one.BlankBytes)
	}
	if one.DuplicateBytes != 15 {
		t.Fatalf("duplicate bytes %d, want 15", one.DuplicateBytes)
	}
	if one.Bytes != int64(len(content)) {
		t.Fatalf("result bytes %d against a content of %d", one.Bytes, len(content))
	}
	if one.TrailingBytes != 2 {
		t.Fatalf("trailing bytes %d, want 2", one.TrailingBytes)
	}
	if one.RemovableBytes() > one.Bytes {
		t.Fatalf("removable %d over a result of %d bytes", one.RemovableBytes(), one.Bytes)
	}
	if one.UniqueLines != 2 {
		t.Fatalf("unique lines %d, want 2", one.UniqueLines)
	}
}

func TestTheArmNeverKeepsMoreThanTheCapAndNeverKeepsMoreLinesThanExist(t *testing.T) {
	result, err := Run(sessionsDir(), artifactsDir())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Prose.Handles == 0 {
		t.Fatal("no artifact handle was found, so the redundancy table has no input")
	}
	for _, row := range append(result.Prose.Rows, result.Prose.Whole) {
		if row.ArmKeptBytes > int64(row.Artifacts*result.ResultBytesCap) {
			t.Fatalf("%s: arm kept %d bytes over %d artifacts at a cap of %d", row.Tool, row.ArmKeptBytes, row.Artifacts, result.ResultBytesCap)
		}
		if row.ArmKeptUniqueLines > row.UniqueLines {
			t.Fatalf("%s: arm kept %d unique lines out of %d", row.Tool, row.ArmKeptUniqueLines, row.UniqueLines)
		}
		if row.RemovableBytes() > row.Bytes {
			t.Fatalf("%s: %d removable bytes out of %d", row.Tool, row.RemovableBytes(), row.Bytes)
		}
	}
}

func TestRtkCappedSavingNeverExceedsTheClaimedOne(t *testing.T) {
	result, err := Run(sessionsDir(), artifactsDir())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Rtk.ClaimedPerSession.Sum == 0 {
		t.Fatal("no bash call was counted, so the rtk row has no input")
	}
	if result.Rtk.CappedPerSession.Sum > result.Rtk.ClaimedPerSession.Sum {
		t.Fatalf("capped %d over claimed %d", result.Rtk.CappedPerSession.Sum, result.Rtk.ClaimedPerSession.Sum)
	}
}

func TestReportOverTheRealCorpus(t *testing.T) {
	result, err := Run(sessionsDir(), artifactsDir())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	rendered := Render(result)
	for _, want := range []string{"calls per session", "tool result tokens per session", "rtk over a whole session", "redundancy in a recorded tool result"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("the report carries no %q section", want)
		}
	}
	t.Log("\n" + rendered)
}
