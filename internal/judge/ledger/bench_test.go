package ledger

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSchemaBumpedForTheBenchName(t *testing.T) {
	if SchemaVersion != BenchSchema {
		t.Fatalf("schema version = %d, want %d, the bench name arrived there", SchemaVersion, BenchSchema)
	}
	if BenchSchema <= FingerprintSchema {
		t.Fatalf("the bench schema is %d and the fingerprint schema is %d, an added field moves the version forward", BenchSchema, FingerprintSchema)
	}
}

func benchRow(at time.Time) Row {
	return Row{
		At:           at,
		Bench:        "sift",
		Point:        "shell_sift@1",
		Questions:    "shell_sift",
		Version:      1,
		Build:        "typesafe/jev-1.13-20260917",
		Model:        "~typesafe/jev-latest",
		StateBuilder: "shell_sift.1",
		State:        json.RawMessage(`{"task":"what is this repository","chunk":"bench/sift/arm.go"}`),
		Answers:      []Answer{{Question: "still_needed", Wording: 1, Kind: AnswerNoul, Noul: 0.91}},
		LatencyMS:    412,
		Cost:         0.0000306,
		RequestID:    "recorded-1",
	}
}

func TestABenchRowIsTellableFromATurnRow(t *testing.T) {
	dir := t.TempDir()
	writer := NewWriter(dir)
	at := time.Date(2026, 9, 22, 9, 0, 0, 0, time.UTC)
	turn, err := writer.Append(sampleRow("tool_gate", VerdictAllow, at))
	if err != nil {
		t.Fatalf("Append the turn row: %v", err)
	}
	bench, err := writer.Append(benchRow(at))
	if err != nil {
		t.Fatalf("Append the bench row: %v", err)
	}

	if turn.Origin() != OriginTurn {
		t.Fatalf("a row with no bench name reads as %q, want %q", turn.Origin(), OriginTurn)
	}
	if bench.Origin() != OriginBench {
		t.Fatalf("a row written by a bench reads as %q, want %q", bench.Origin(), OriginBench)
	}

	written, err := json.Marshal(turn)
	if err != nil {
		t.Fatalf("marshal the turn row: %v", err)
	}
	if strings.Contains(string(written), `"bench"`) {
		t.Fatalf("a turn row carries a bench key: %s", written)
	}

	reader := NewReader(dir)
	for _, each := range []struct {
		origin Origin
		want   int
	}{{OriginAny, 2}, {OriginTurn, 1}, {OriginBench, 1}} {
		report, err := reader.Each(Filter{Origin: each.origin}, func(Row) error { return nil })
		if err != nil {
			t.Fatalf("Each %q: %v", each.origin, err)
		}
		if report.Matched != each.want {
			t.Fatalf("origin %q matched %d rows, want %d", each.origin, report.Matched, each.want)
		}
	}

	stored, found, err := reader.ByID(bench.ID)
	if err != nil || !found {
		t.Fatalf("ByID %s: found=%v, %v", bench.ID, found, err)
	}
	if stored.Bench != "sift" || stored.Point != "shell_sift@1" || stored.Build != "typesafe/jev-1.13-20260917" {
		t.Fatalf("the bench row read back as bench=%q point=%q build=%q", stored.Bench, stored.Point, stored.Build)
	}
	if len(stored.Answers) != 1 || stored.Answers[0].Kind != AnswerNoul || stored.Answers[0].Noul != 0.91 {
		t.Fatalf("the bench row read back with answers %+v", stored.Answers)
	}
	state, err := reader.State(stored)
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if !strings.Contains(string(state), "bench/sift/arm.go") {
		t.Fatalf("the state read back as %s", state)
	}
}

func TestABenchRowDoesNotMoveTheCountsATurnRowIsSummarisedBy(t *testing.T) {
	turnsOnly, benchToo := t.TempDir(), t.TempDir()
	at := time.Date(2026, 9, 22, 9, 0, 0, 0, time.UTC)
	for _, dir := range []string{turnsOnly, benchToo} {
		if _, err := NewWriter(dir).Append(sampleRow("tool_gate", VerdictAllow, at)); err != nil {
			t.Fatalf("Append the turn row: %v", err)
		}
	}
	if _, err := NewWriter(benchToo).Append(benchRow(at)); err != nil {
		t.Fatalf("Append the bench row: %v", err)
	}

	apart, err := Summary(turnsOnly)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	together, err := Summary(benchToo)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if apart.Rows != 1 {
		t.Fatalf("a ledger holding one turn row summarises %d rows", apart.Rows)
	}
	if together.Rows != 2 {
		t.Fatalf("a directory holding a turn row and a bench row summarises %d rows, want both: the separation is the filter, not the count", together.Rows)
	}
}

func TestABenchRowsLargeStateIsElidedAndReadBackThroughTheGuardedReader(t *testing.T) {
	dir := t.TempDir()
	row := benchRow(time.Date(2026, 9, 22, 9, 0, 0, 0, time.UTC))
	body := json.RawMessage(`{"task":"what is this repository","chunk":"` + strings.Repeat("z", 8*1024) + `"}`)
	row.State = body

	stored, err := NewWriter(dir).Append(row)
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if stored.StateElision == nil {
		t.Fatalf("a %d byte bench state stayed inline", len(body))
	}
	if stored.State != nil {
		t.Fatalf("an elided bench row still carries %d bytes of state on the row", len(stored.State))
	}
	if stored.StateElision.Bytes != len(body) {
		t.Fatalf("the elision says %d bytes, the state is %d", stored.StateElision.Bytes, len(body))
	}
	read, err := NewReader(dir).State(stored)
	if err != nil {
		t.Fatalf("the bench state elided to %s does not read back: %v", stored.StateElision.File, err)
	}
	if string(read) != string(body) {
		t.Fatalf("the bench state read back as %d bytes, want %d", len(read), len(body))
	}
}
