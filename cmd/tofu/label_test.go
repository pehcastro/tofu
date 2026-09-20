package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"tofu/internal/judge/ledger"
)

func TestParseOutcomeAcceptsTheClosedSet(t *testing.T) {
	for _, want := range []outcome{outcomeAllow, outcomeAsk, outcomeDeny} {
		got, err := parseOutcome(string(want))
		if err != nil {
			t.Fatalf("parseOutcome(%q): %v", want, err)
		}
		if got != want {
			t.Fatalf("parseOutcome(%q) = %q", want, got)
		}
	}
}

func TestParseOutcomeRejectsAnUnknownValue(t *testing.T) {
	if _, err := parseOutcome("maybe"); err == nil {
		t.Fatal("parseOutcome accepted a value outside allow, ask, deny")
	}
}

func seedRow(t *testing.T, dir string, at time.Time) ledger.Row {
	t.Helper()
	row, err := ledger.NewWriterWithClock(dir, func() time.Time { return at }).Append(ledger.Row{
		Point:     "tool_gate",
		Questions: "tool_gate",
		Version:   1,
		Model:     "~typesafe/jev-latest",
		StateHash: "deadbeef",
		Verdict:   ledger.VerdictAsk,
	})
	if err != nil {
		t.Fatalf("seeding a row: %v", err)
	}
	return row
}

func TestLabelAttachesTheOutcomeAndWhyShowsIt(t *testing.T) {
	t.Chdir(t.TempDir())
	dir, err := ledger.Dir()
	if err != nil {
		t.Fatalf("ledger.Dir: %v", err)
	}
	row := seedRow(t, dir, time.Now())

	var out, errOut bytes.Buffer
	code := labelVerb([]string{row.ID, "deny"}, &out, &errOut, time.Now)
	if code != exitOK {
		t.Fatalf("exit = %d, want %d, stderr %s", code, exitOK, errOut.String())
	}

	found, ok, err := ledger.NewReader(dir).ByID(row.ID)
	if err != nil {
		t.Fatalf("reading the row back: %v", err)
	}
	if !ok {
		t.Fatalf("row %q not found", row.ID)
	}
	if found.Outcome == nil {
		t.Fatal("the row carries no outcome")
	}
	if found.Outcome.Kind != outcomeKindHandLabeled || found.Outcome.Detail != "deny" {
		t.Fatalf("outcome = %+v", found.Outcome)
	}
}

func TestLabelRefusesARowThatAlreadyHasAnOutcome(t *testing.T) {
	t.Chdir(t.TempDir())
	dir, err := ledger.Dir()
	if err != nil {
		t.Fatalf("ledger.Dir: %v", err)
	}
	row := seedRow(t, dir, time.Now())

	var out, errOut bytes.Buffer
	if code := labelVerb([]string{row.ID, "ask"}, &out, &errOut, time.Now); code != exitOK {
		t.Fatalf("first label failed: exit %d stderr %s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	code := labelVerb([]string{row.ID, "deny"}, &out, &errOut, time.Now)
	if code != exitUsage {
		t.Fatalf("exit = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(errOut.String(), "ask") {
		t.Fatalf("refusal does not name the existing outcome: %s", errOut.String())
	}
}

func TestLabelLastLabelsTheMostRecentRow(t *testing.T) {
	t.Chdir(t.TempDir())
	dir, err := ledger.Dir()
	if err != nil {
		t.Fatalf("ledger.Dir: %v", err)
	}
	older := seedRow(t, dir, time.Now().Add(-time.Hour))
	newer := seedRow(t, dir, time.Now())

	var out, errOut bytes.Buffer
	code := labelVerb([]string{"--last", "allow"}, &out, &errOut, time.Now)
	if code != exitOK {
		t.Fatalf("exit = %d, want %d, stderr %s", code, exitOK, errOut.String())
	}

	newRow, _, err := ledger.NewReader(dir).ByID(newer.ID)
	if err != nil {
		t.Fatalf("reading the newer row: %v", err)
	}
	if newRow.Outcome == nil || newRow.Outcome.Detail != "allow" {
		t.Fatalf("--last labelled %+v, want the newer row", newRow.Outcome)
	}
	oldRow, _, err := ledger.NewReader(dir).ByID(older.ID)
	if err != nil {
		t.Fatalf("reading the older row: %v", err)
	}
	if oldRow.Outcome != nil {
		t.Fatalf("--last also labelled the older row: %+v", oldRow.Outcome)
	}
}
