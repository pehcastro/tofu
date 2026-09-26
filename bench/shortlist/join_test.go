package shortlist

import (
	"os"
	"testing"
)

func TestTheSearchJoinCountedFromRecordedSessions(t *testing.T) {
	if _, err := os.Stat(sessionsDir()); err != nil {
		t.Skipf("no recorded sessions at %s on this machine: %v", sessionsDir(), err)
	}
	rows, counts, err := BuildJoinCorpus(sessionsDir())
	if err != nil {
		t.Fatalf("BuildJoinCorpus: %v", err)
	}
	t.Logf("%d sessions, %d searches, %d hit, %d unoffered (%.1f%% of acted-on searches), %d none: %d distinct-task hit rows built against the floor of 12",
		counts.Sessions, counts.Searches, counts.Hits, counts.Unoffered, 100*counts.UnofferedRate(), counts.None, counts.Distinct)
	leaky := leakyRows(rows)
	t.Logf("%d of %d join rows name their own label in their own task", len(leaky), len(rows))
	for _, row := range rows {
		t.Logf("%-40s label=%v task=%q", row.TurnID, row.Label, row.Task)
	}
}
