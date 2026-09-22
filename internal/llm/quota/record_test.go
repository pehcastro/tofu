package quota

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func servingCodexPoller(t *testing.T, record func(Reading) error) *Poller {
	t.Helper()
	return recordingCodexPoller(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(servingCodexBody("Plus"))
	}, record)
}

func TestAFetchWritesOneReadingAndACachedPollWritesNone(t *testing.T) {
	var written []Reading
	poller := servingCodexPoller(t, func(reading Reading) error {
		written = append(written, reading)
		return nil
	})
	account := Account{Provider: CodexSub, AccountID: alphaAccount, Row: 7, Credential: stubCredential{token: "token"}}
	for range 2 {
		if _, err := poller.Poll(context.Background(), account); err != nil {
			t.Fatalf("polling: %v", err)
		}
	}
	if len(written) != 1 {
		t.Fatalf("two polls one after the other wrote %d readings, want the fetch to write and the cache hit to write none",
			len(written))
	}
	reading := written[0]
	if reading.Provider != CodexSub || reading.Account != 7 || !reading.At.Equal(recordedNow) {
		t.Fatalf("the reading reads as %+v", reading)
	}
	if len(reading.Windows) != 1 || reading.Windows[0].ID != "5h" || reading.Windows[0].Used != 0.12 {
		t.Fatalf("the reading's windows read as %+v", reading.Windows)
	}
}

func TestAReadingThatCannotBeWrittenLeavesThePollUnchanged(t *testing.T) {
	refused := errors.New("quota: the reading directory is read only")
	poller := servingCodexPoller(t, func(Reading) error { return refused })
	account := Account{Provider: CodexSub, AccountID: alphaAccount, Row: 7, Credential: stubCredential{token: "token"}}

	report, err := poller.Poll(context.Background(), account)
	if err != nil {
		t.Fatalf("a poll failed because its reading could not be written: %v", err)
	}
	if report.Plan != "Plus" || len(report.Windows) != 1 {
		t.Fatalf("the report came back as %+v", report)
	}
}

func TestAnAppendedReadingIsOneLinePerPollAndSkipsAWindowWithNoUse(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "quota")
	report := Report{
		Provider:  ClaudeSub,
		FetchedAt: recordedNow,
		Windows: []Window{
			{ID: "5h", Used: Used{Fraction: 0.25, Reported: true}, ResetsAt: recordedNow.Add(time.Hour)},
			{ID: "opus:7d", Used: Used{}},
		},
	}
	for _, account := range []int64{1, 2} {
		if err := AppendReading(dir, ReadingOf(account, report)); err != nil {
			t.Fatalf("appending a reading: %v", err)
		}
	}

	name := filepath.Join(dir, recordedNow.UTC().Format(time.DateOnly)+".jsonl")
	body, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	if len(lines) != 2 {
		t.Fatalf("two polls left %d lines in %s:\n%s", len(lines), name, body)
	}
	var read Reading
	if err := json.Unmarshal([]byte(lines[1]), &read); err != nil {
		t.Fatalf("the second line is not a reading: %v", err)
	}
	if read.Account != 2 || len(read.Windows) != 1 || read.Windows[0].ID != "5h" {
		t.Fatalf("the second line reads as %+v, want account 2 and only the window the vendor reported use for", read)
	}
}

func TestAReadingCannotBeWrittenWhereAFileIsInTheWay(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "quota")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AppendReading(blocked, ReadingOf(1, Report{Provider: ClaudeSub, FetchedAt: recordedNow})); err == nil {
		t.Fatal("appending into a path held by a file reported success")
	}
}
