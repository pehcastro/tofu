package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func recordedReadings(t *testing.T, lines ...string) string {
	t.Helper()
	isolatedHomeAndProject(t)
	dir, err := quotaReadingDir()
	if err != nil {
		t.Fatalf("quotaReadingDir: %v", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("seeding the reading dir: %v", err)
	}
	body := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "2026-09-21.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatalf("seeding a reading: %v", err)
	}
	return dir
}

const (
	firstRecordedReading = `{"provider":"claude-sub","account":4242,"at":"2026-09-21T15:04:05Z",` +
		`"windows":[{"id":"5h","used_fraction":0.25,"resets_at":"2026-09-21T18:00:00Z"}]}`
	secondRecordedReading = `{"provider":"claude-sub","account":4242,"at":"2026-09-21T18:04:05Z",` +
		`"windows":[{"id":"5h","used_fraction":0.4,"resets_at":"2026-09-21T21:00:00Z"}]}`
)

func TestUsageHistoryShowsEveryReadingWithTheMomentItWasCaptured(t *testing.T) {
	recordedReadings(t, firstRecordedReading, secondRecordedReading)
	var out, errOut bytes.Buffer
	if code := usageVerb([]string{historyFlag}, &out, &errOut, plain); code != exitOK {
		t.Fatalf("usage --history exited %d: %s", code, errOut.String())
	}
	want := "2 readings recorded\n" +
		"2026-09-21T15:04:05Z  claude-sub  5h  25%\n" +
		"2026-09-21T18:04:05Z  claude-sub  5h  40%\n"
	if out.String() != want {
		t.Fatalf("usage --history printed\n%q\nwant\n%q", out.String(), want)
	}
}

func TestUsageHistoryCreatesNoFile(t *testing.T) {
	isolatedHomeAndProject(t)
	dir, err := quotaReadingDir()
	if err != nil {
		t.Fatalf("quotaReadingDir: %v", err)
	}
	var out, errOut bytes.Buffer
	if code := usageVerb([]string{historyFlag}, &out, &errOut, plain); code != exitOK {
		t.Fatalf("usage --history exited %d: %s", code, errOut.String())
	}
	if out.String() != "0 readings recorded\n" {
		t.Fatalf("usage --history with nothing recorded printed %q", out.String())
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("the read path created %s", dir)
	}
}

func TestUsageHistoryPrintsNoSecretCarriedByTheReading(t *testing.T) {
	secrets := []string{"not-a-real-token-a", "not-a-real-account-a", "not-a-real-key-a", "4242"}
	leaky := `{"provider":"claude-sub","account":4242,"at":"2026-09-21T15:04:05Z",` +
		`"access_token":"` + secrets[0] + `","account_id":"` + secrets[1] + `","api_key":"` + secrets[2] + `",` +
		`"windows":[{"id":"5h","used_fraction":0.25,"resets_at":"2026-09-21T18:00:00Z"}]}`
	recordedReadings(t, leaky)

	for _, args := range [][]string{{historyFlag}, {historyFlag, jsonFlag}} {
		var out, errOut bytes.Buffer
		if code := usageVerb(args, &out, &errOut, plain); code != exitOK {
			t.Fatalf("usage %v exited %d: %s", args, code, errOut.String())
		}
		if !strings.Contains(out.String(), "claude-sub") {
			t.Fatalf("usage %v did not read the seeded reading:\n%s", args, out.String())
		}
		for _, secret := range secrets {
			if strings.Contains(out.String(), secret) {
				t.Fatalf("usage %v printed %q", args, secret)
			}
		}
	}
}

func TestUsageHistoryJSONCarriesTheMomentAndTheWindow(t *testing.T) {
	recordedReadings(t, firstRecordedReading)
	var out, errOut bytes.Buffer
	if code := usageVerb([]string{historyFlag, jsonFlag}, &out, &errOut, plain); code != exitOK {
		t.Fatalf("usage --history --json exited %d: %s", code, errOut.String())
	}
	var report quotaHistoryReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("reading the history report: %v, body %q", err, out.String())
	}
	if len(report.Readings) != 1 {
		t.Fatalf("the report carries %d readings, want 1", len(report.Readings))
	}
	row := report.Readings[0]
	if row.At.Format("2006-01-02T15:04:05Z") != "2026-09-21T15:04:05Z" || row.Window != "5h" || row.Used != 0.25 {
		t.Fatalf("the reading reads as %+v", row)
	}
}
