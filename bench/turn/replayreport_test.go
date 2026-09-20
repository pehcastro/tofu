package turn

import (
	"os"
	"testing"
	"time"
)

const sessionsDir = "../../.tofu/sessions"

func TestReplayOverRealRecordedSessionsWritesTheDatedReport(t *testing.T) {
	if _, err := os.Stat(sessionsDir); os.IsNotExist(err) {
		t.Skipf("no %s on this machine, nothing to replay", sessionsDir)
	}

	corpus, err := ReadCorpus(sessionsDir)
	if err != nil {
		t.Fatalf("reading the corpus: %v", err)
	}
	if len(corpus.Turns) < 8 {
		t.Fatalf("the acceptance line asks for at least 8 real recorded turns, %s only found %d", sessionsDir, len(corpus.Turns))
	}

	host, err := os.Hostname()
	if err != nil {
		t.Fatalf("no hostname for the conditions line: %v", err)
	}
	report := BuildReplayReport("2026-09-21", host, corpus)

	if report.LayeredTotal.Refusals == 0 {
		t.Fatal("the acceptance line asks refusals be counted per arm, and this corpus should carry real ones")
	}
	if report.AsRecordedTotal.BytesReturned == 0 {
		t.Fatal("the acceptance line asks bytes returned be counted per arm, and this corpus should carry real ones")
	}
	if report.LayeredTotal.AnswersChanged == 0 {
		t.Fatal("the acceptance line asks whether the answer changed be counted per arm, and this corpus should carry at least one changed answer")
	}
	for _, m := range report.Mechanisms {
		if m.Verdict == "" {
			t.Fatalf("mechanism %s carries no verdict", m.Name)
		}
	}

	body := RenderReplayReport(report)
	path := ReplayReportFilename(report.Date)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}

	for _, turn := range report.PerTurn {
		t.Logf("replayed %s", turn.ID)
	}
	t.Logf("%s, generated at %s", corpus.String(), time.Now().Format(time.RFC3339))
	t.Logf("wrote %s", path)
}
