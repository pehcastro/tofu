package corpus

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHowManyTurnsInTheCorpusCarryARetry(t *testing.T) {
	entries, err := os.ReadDir(recordedSessionsDir())
	if err != nil {
		t.Skipf("no %s on this machine: %v", recordedSessionsDir(), err)
	}
	read, retried, unrecorded := 0, 0, 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		recorded, err := ReadTurnDir(filepath.Join(recordedSessionsDir(), entry.Name()))
		if err != nil {
			continue
		}
		read++
		carries, blank := false, false
		for _, step := range recorded.Steps {
			switch {
			case step.Attempt > 1:
				carries = true
			case step.Attempt == 0:
				blank = true
			}
		}
		if carries {
			retried++
		}
		if blank {
			unrecorded++
		}
	}
	if read == 0 {
		t.Skip("no turn of the header and jsonl schema on this machine")
	}
	t.Logf("%d turns read, %d carry a step recorded as a retry, %d carry a step written before the field existed", read, retried, unrecorded)
}

func TestAStepReadsTheAttemptFromTheEventThatCarriesIt(t *testing.T) {
	dir := t.TempDir()
	body := `{"id":"e1","attempt":2,"kind":"step","body":{"index":1,"tool_calls":[{"id":"c1","tool":"read"}]}}
{"id":"e2","attempt":1,"kind":"outcome","body":{"id":"turn-1","wall_clock_ms":12}}
`
	if err := os.WriteFile(filepath.Join(dir, "body.jsonl"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	recorded, err := ReadTurnDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(recorded.Steps) != 1 || recorded.Steps[0].Attempt != 2 {
		t.Fatalf("the reader read %+v, want one step carrying attempt 2", recorded.Steps)
	}
}
