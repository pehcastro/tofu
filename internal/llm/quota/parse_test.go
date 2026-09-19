package quota

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

var recordedNow = time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)

func recordedBody(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading the recorded body: %v", err)
	}
	return raw
}

func windowByID(t *testing.T, report Report, id string) Window {
	t.Helper()
	for _, window := range report.Windows {
		if window.ID == id {
			return window
		}
	}
	t.Fatalf("no %q window in %v", id, report.Windows)
	return Window{}
}

func TestOnlyAnExhaustedWindowWithAFutureResetAuthorizesWaiting(t *testing.T) {
	spent := Window{ID: "7d", Used: Used{Fraction: 1, Reported: true}}
	timed := spent
	timed.ResetsAt = recordedNow.Add(2 * time.Hour)

	withReset := Report{Provider: Anthropic, Windows: []Window{timed}}
	until, ok := withReset.WaitUntil(recordedNow)
	if !ok || !until.Equal(timed.ResetsAt) {
		t.Fatalf("an exhausted window with a future reset gave %v %v, want the reset", until, ok)
	}

	withoutReset := Report{Provider: Anthropic, Windows: []Window{spent}}
	if _, ok := withoutReset.WaitUntil(recordedNow); ok {
		t.Fatal("an exhausted window with no reset authorized a wait")
	}
	if !withoutReset.Exhausted() {
		t.Fatal("an exhausted window with no reset stopped reading as exhausted")
	}

	permanentBesideTimed := Report{Provider: Anthropic, Windows: []Window{timed, spent}}
	if _, ok := permanentBesideTimed.WaitUntil(recordedNow); ok {
		t.Fatal("a permanent cap beside a timed window authorized a wait")
	}
}

func TestTheUsageEndpointPayloadsCarryEveryWindow(t *testing.T) {
	anthropic, err := FromAnthropicUsage(recordedBody(t, "anthropic-usage.json"), recordedNow)
	if err != nil {
		t.Fatalf("parsing the anthropic usage payload: %v", err)
	}
	if got := windowByID(t, anthropic, "5h").Used.Fraction; got != 0.17 {
		t.Fatalf("the anthropic 5h utilization read %v, want 0.17", got)
	}
	scoped := windowByID(t, anthropic, "7d:fable")
	if scoped.State() != StateExhausted {
		t.Fatalf("an inactive scoped limit at 100 percent read %+v, want exhausted", scoped)
	}

	codex, err := FromCodexUsage(recordedBody(t, "codex-usage.json"), recordedNow)
	if err != nil {
		t.Fatalf("parsing the codex usage payload: %v", err)
	}
	if !codex.Exhausted() {
		t.Fatal("a spent codex plan window read as serving")
	}
	if got := windowByID(t, codex, "5h").ResetsAt; !got.Equal(recordedNow.Add(time.Hour)) {
		t.Fatalf("a reset_after_seconds window read %v, want an hour on", got)
	}
	if got := windowByID(t, codex, "7d").Used.Fraction; got != 0.425 {
		t.Fatalf("the codex secondary window read %v, want 0.425", got)
	}

	funded, err := FromCodexUsage(recordedBody(t, "codex-usage-credits.json"), recordedNow)
	if err != nil {
		t.Fatalf("parsing the credit-funded codex payload: %v", err)
	}
	if funded.Exhausted() {
		t.Fatal("a credit-funded account read as exhausted, which parks a working account until the weekly reset")
	}
}
