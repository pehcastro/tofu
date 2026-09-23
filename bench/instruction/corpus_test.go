package instruction

import (
	"os"
	"testing"
)

func loadOrSkip(t *testing.T) Corpus {
	t.Helper()
	if _, err := os.Stat(SessionsDir); err != nil {
		t.Skipf("skipped: %s is not on this machine, so there is no recorded turn to read", SessionsDir)
	}
	loaded, err := Load(SessionsDir)
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
