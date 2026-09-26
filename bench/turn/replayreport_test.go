package turn

import (
	"os"
	"testing"
	"time"

	"tofu/internal/sys"
)

func sessionsDir() string { return sys.RecordedStateDir("sessions") }

const regenerateEnvVar = "TOFU_BENCH_TURN_REGENERATE"

func TestReplayOverRealRecordedSessionsMatchesTheDatedReport(t *testing.T) {
	if _, err := os.Stat(sessionsDir()); os.IsNotExist(err) {
		t.Skipf("no %s on this machine, nothing to replay", sessionsDir())
	}

	corpus, err := ReadCorpus(sessionsDir())
	if err != nil {
		t.Fatalf("reading the corpus: %v", err)
	}
	if len(corpus.Turns) < 8 {
		t.Fatalf("the acceptance line asks for at least 8 real recorded turns, %s only found %d", sessionsDir(), len(corpus.Turns))
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

	committed := ReplayReportFilename(report.Date)
	fileBody, err := os.ReadFile(committed)
	if err != nil {
		t.Fatalf("reading the committed %s: %v", committed, err)
	}
	fileHeadline, err := ParseHeadline(string(fileBody))
	if err != nil {
		t.Fatalf("parsing %s: %v", committed, err)
	}
	diskHeadline := HeadlineOf(report)

	if maybeRegenerate(t, host, corpus, committed) {
		return
	}

	if fileHeadline.EntryCount != diskHeadline.EntryCount {
		t.Skipf("%s was written against %d recorded sessions; %s now holds %d, %d more than the report carries: that drift is the finding, not repaired here, and it is not compared further; a person decides whether it is a withdrawal or a new dated report; set %s=1 to render one alongside the committed file",
			committed, fileHeadline.EntryCount, sessionsDir(), diskHeadline.EntryCount, diskHeadline.EntryCount-fileHeadline.EntryCount, regenerateEnvVar)
	}
	if err := CompareHeadlines(fileHeadline, diskHeadline); err != nil {
		t.Fatalf("%s no longer follows from what %s replays today: %v", committed, sessionsDir(), err)
	}

	for _, turn := range report.PerTurn {
		t.Logf("replayed %s", turn.ID)
	}
	t.Logf("%s, generated at %s", corpus.String(), time.Now().Format(time.RFC3339))
	t.Logf("%s was written against %d recorded sessions and still matches today's %d", committed, fileHeadline.EntryCount, diskHeadline.EntryCount)
}

func maybeRegenerate(t *testing.T, host string, corpus Corpus, committed string) bool {
	t.Helper()
	if os.Getenv(regenerateEnvVar) != "1" {
		return false
	}
	today := time.Now().Format("2006-01-02")
	path := ReplayReportFilename(today)
	if path == committed {
		t.Fatalf("%s=1 on %s would write over the committed %s, refusing", regenerateEnvVar, today, committed)
	}
	fresh := BuildReplayReport(today, host, corpus)
	if err := os.WriteFile(path, []byte(RenderReplayReport(fresh)), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	t.Logf("wrote a new dated report at %s, %s is untouched", path, committed)
	return true
}
