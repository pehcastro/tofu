package ledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func sampleRow(point string, verdict Verdict, at time.Time) Row {
	return Row{
		At:        at,
		Point:     point,
		Questions: "tool_gate",
		Version:   4,
		Build:     "jev-1.13.0-2026-09-11",
		Model:     "~typesafe/jev-latest",
		StateHash: "9d1f0c",
		Answers: []Answer{{
			Question: "risk",
			Wording:  4,
			Kind:     AnswerChoice,
			Choice:   "force-push to a shared branch",
			Dist: []Slice{
				{Option: "harmless", P: 0.01},
				{Option: "reversible", P: 0.04},
				{Option: "costly", P: 0.11},
				{Option: "force-push to a shared branch", P: 0.84},
			},
		}},
		Verdict:   verdict,
		LatencyMS: 658,
		Cost:      0.000114,
		RequestID: "or-req-8f2c",
	}
}

func TestAppendFillsIdSchemaAndLeavesTheOutcomeEmpty(t *testing.T) {
	dir := t.TempDir()
	at := time.Date(2026, 9, 18, 11, 30, 0, 0, time.UTC)
	filled := Outcome{Kind: "reverted"}
	row := sampleRow("tool_gate", VerdictDeny, at)
	row.Outcome = &filled

	stored, err := NewWriter(dir).Append(row)
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if stored.Schema != SchemaVersion {
		t.Errorf("schema = %d, want %d", stored.Schema, SchemaVersion)
	}
	if !strings.HasPrefix(stored.ID, "2026-09-18-") {
		t.Errorf("id = %q, it must start with the day so a backfill finds its file", stored.ID)
	}
	if stored.Outcome != nil {
		t.Errorf("the outcome slot must be empty at write time, got %+v", stored.Outcome)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "2026-09-18.jsonl"))
	if err != nil {
		t.Fatalf("the day file is named for the day: %v", err)
	}
	if strings.Count(string(raw), "\n") != 1 {
		t.Errorf("one row is one line, got %q", raw)
	}
	if strings.Contains(string(raw), "outcome") {
		t.Errorf("a written row carries no outcome key, got %s", raw)
	}
}

func TestAppendNeverRewritesADayFile(t *testing.T) {
	dir := t.TempDir()
	writer := NewWriter(dir)
	at := time.Date(2026, 9, 18, 11, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		if _, err := writer.Append(sampleRow("tool_gate", VerdictAsk, at.Add(time.Duration(i)*time.Minute))); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	raw, err := os.ReadFile(filepath.Join(dir, "2026-09-18.jsonl"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("3 appends make 3 lines, got %d", len(lines))
	}
	for i, line := range lines {
		var row Row
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Errorf("line %d does not decode: %v", i+1, err)
		}
	}
}

func TestBackfillRefusesAnIdWithoutADay(t *testing.T) {
	writer := NewWriter(t.TempDir())
	if err := writer.Backfill("not-an-id", Outcome{Kind: "reverted"}); err == nil {
		t.Fatal("a backfill against an id with no day must fail, it got no error")
	}
}

func TestBackfillRefusesAnOutcomeWithoutAKind(t *testing.T) {
	writer := NewWriter(t.TempDir())
	if err := writer.Backfill("2026-09-18-aabb", Outcome{}); err == nil {
		t.Fatal("an outcome with no kind must fail, it got no error")
	}
}

func TestAppendRefusesAnUnknownModeValue(t *testing.T) {
	dir := t.TempDir()
	row := sampleRow("tool_gate", VerdictDeny, time.Date(2026, 9, 18, 11, 0, 0, 0, time.UTC))
	row.Reason = &Reason{Question: "risk", Comparison: "risk_deny_at", Threshold: 2.5, Value: 3.0, Mode: Mode("bogus")}
	if _, err := NewWriter(dir).Append(row); err == nil {
		t.Fatal("a row whose mode is neither shadow nor enforced must fail to append, it got no error")
	}
}

func TestAppendRefusesAModeLeftUnset(t *testing.T) {
	dir := t.TempDir()
	row := sampleRow("tool_gate", VerdictDeny, time.Date(2026, 9, 18, 11, 0, 0, 0, time.UTC))
	row.Reason = &Reason{Question: "risk", Comparison: "risk_deny_at", Threshold: 2.5, Value: 3.0}
	if _, err := NewWriter(dir).Append(row); err == nil {
		t.Fatal("a judged row must state shadow or enforced, an unset mode must fail to append")
	}
}
