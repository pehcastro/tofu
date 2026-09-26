package instruction

import (
	"os"
	"testing"

	"tofu/internal/sys"
)

func loadOrSkip(t *testing.T) Corpus {
	t.Helper()
	sessionsDir := sys.RecordedStateDir("sessions")
	if _, err := os.Stat(sessionsDir); err != nil {
		t.Skipf("skipped: %s is not on this machine, so there is no recorded turn to read", sessionsDir)
	}
	loaded, err := Load(sessionsDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Rows) == 0 {
		t.Skip("skipped: every labelled case is missing from the sessions on this machine")
	}
	return loaded
}

func TestTheCorpusIsReadFromRecordedSessionsAtRunTime(t *testing.T) {
	loaded := loadOrSkip(t)

	shaped, data := 0, 0
	for _, row := range loaded.Rows {
		if row.Turn == "" || row.Key == "" {
			t.Fatalf("a row carries no source: %+v", row)
		}
		if row.InstructionShaped {
			shaped++
		} else {
			data++
		}
	}
	for _, s := range loaded.Skipped {
		t.Logf("skipped: %s", s)
	}
	t.Logf("%d labelled cases named, %d read from .tofu/sessions today, %d skipped, %d instruction-shaped, %d data",
		len(labels), len(loaded.Rows), len(loaded.Skipped), shaped, data)
}
