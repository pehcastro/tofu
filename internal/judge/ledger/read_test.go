package ledger

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"tofu/internal/sys"
)

func canonicalWithTheOutcomeRemoved(t *testing.T, row Row) []byte {
	t.Helper()
	canon, err := Canonical(row)
	if err != nil {
		t.Fatalf("Canonical: %v", err)
	}
	var tree map[string]any
	if err := json.Unmarshal(canon, &tree); err != nil {
		t.Fatalf("decode: %v", err)
	}
	delete(tree, "outcome")
	stripped, err := Canonical(tree)
	if err != nil {
		t.Fatalf("Canonical: %v", err)
	}
	return stripped
}

func readAll(t *testing.T, dir string, filter Filter) ([]Row, Report) {
	t.Helper()
	var rows []Row
	report, err := NewReader(dir).Each(filter, func(row Row) error {
		rows = append(rows, row)
		return nil
	})
	if err != nil {
		t.Fatalf("Each: %v", err)
	}
	return rows, report
}

func TestReadEmptyLedgerIsNotAnError(t *testing.T) {
	rows, report := readAll(t, filepath.Join(t.TempDir(), "never-written"), Filter{})
	if len(rows) != 0 || report.Files != 0 || report.Scanned != 0 {
		t.Fatalf("an unused project has an empty ledger, got %d rows and %+v", len(rows), report)
	}
}

func TestFiltersOnPointVerdictDateAndVersion(t *testing.T) {
	dir := t.TempDir()
	writer := NewWriter(dir)
	days := []time.Time{
		time.Date(2026, 9, 16, 9, 0, 0, 0, time.UTC),
		time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC),
		time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC),
	}
	for _, day := range days {
		for _, point := range []string{"tool_gate", "recall_drop"} {
			row := sampleRow(point, VerdictAsk, day)
			if point == "tool_gate" {
				row.Verdict = VerdictDeny
				row.Version = 4
			} else {
				row.Version = 2
			}
			if _, err := writer.Append(row); err != nil {
				t.Fatalf("Append: %v", err)
			}
		}
	}

	all, report := readAll(t, dir, Filter{})
	if len(all) != 6 || report.Files != 3 {
		t.Fatalf("6 rows over 3 files, got %d rows and %+v", len(all), report)
	}
	byPoint, _ := readAll(t, dir, Filter{Point: "tool_gate"})
	if len(byPoint) != 3 {
		t.Errorf("point filter: got %d rows, want 3", len(byPoint))
	}
	byVerdict, _ := readAll(t, dir, Filter{Verdict: VerdictAsk})
	if len(byVerdict) != 3 {
		t.Errorf("verdict filter: got %d rows, want 3", len(byVerdict))
	}
	byVersion, _ := readAll(t, dir, Filter{Version: 2})
	if len(byVersion) != 3 {
		t.Errorf("version filter: got %d rows, want 3", len(byVersion))
	}
	byDate, dateReport := readAll(t, dir, Filter{Since: days[2]})
	if len(byDate) != 2 {
		t.Errorf("date filter: got %d rows, want 2", len(byDate))
	}
	if dateReport.Files != 1 {
		t.Errorf("a one day filter opens one file, it opened %d", dateReport.Files)
	}
}

func TestReaderFiltersOnMode(t *testing.T) {
	dir := t.TempDir()
	writer := NewWriter(dir)
	at := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	for i := 0; i < 4; i++ {
		row := sampleRow("tool_gate", VerdictAsk, at.Add(time.Duration(i)*time.Minute))
		row.Reason = &Reason{Question: "risk", Comparison: "risk_ask_at", Mode: ModeShadow}
		if _, err := writer.Append(row); err != nil {
			t.Fatalf("Append shadow %d: %v", i, err)
		}
	}
	for i := 0; i < 2; i++ {
		row := sampleRow("tool_gate", VerdictDeny, at.Add(time.Duration(10+i)*time.Minute))
		row.Reason = &Reason{Question: "risk", Comparison: "risk_deny_at", Mode: ModeEnforced}
		if _, err := writer.Append(row); err != nil {
			t.Fatalf("Append enforced %d: %v", i, err)
		}
	}
	row := sampleRow("tool_gate", VerdictAsk, at.Add(20*time.Minute))
	if _, err := writer.Append(row); err != nil {
		t.Fatalf("Append no-reason: %v", err)
	}

	all, _ := readAll(t, dir, Filter{})
	if len(all) != 7 {
		t.Fatalf("7 rows written, read %d", len(all))
	}
	shadow, _ := readAll(t, dir, Filter{Mode: ModeShadow})
	if len(shadow) != 4 {
		t.Fatalf("shadow filter: got %d rows, want 4", len(shadow))
	}
	enforced, _ := readAll(t, dir, Filter{Mode: ModeEnforced})
	if len(enforced) != 2 {
		t.Fatalf("enforced filter: got %d rows, want 2", len(enforced))
	}
}

func TestReaderFiltersOnTurnID(t *testing.T) {
	dir := t.TempDir()
	writer := NewWriter(dir)
	at := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		row := sampleRow("tool_gate", VerdictAsk, at.Add(time.Duration(i)*time.Minute))
		row.TurnID = "turn-a"
		if _, err := writer.Append(row); err != nil {
			t.Fatalf("Append turn-a %d: %v", i, err)
		}
	}
	row := sampleRow("tool_gate", VerdictAsk, at.Add(10*time.Minute))
	row.TurnID = "turn-b"
	if _, err := writer.Append(row); err != nil {
		t.Fatalf("Append turn-b: %v", err)
	}
	row = sampleRow("tool_gate", VerdictAsk, at.Add(20*time.Minute))
	if _, err := writer.Append(row); err != nil {
		t.Fatalf("Append no-turn: %v", err)
	}

	all, _ := readAll(t, dir, Filter{})
	if len(all) != 5 {
		t.Fatalf("5 rows written, read %d", len(all))
	}
	turnA, _ := readAll(t, dir, Filter{TurnID: "turn-a"})
	if len(turnA) != 3 {
		t.Fatalf("turn-a filter: got %d rows, want 3", len(turnA))
	}
	for _, row := range turnA {
		if row.TurnID != "turn-a" {
			t.Fatalf("filter returned a row with turn_id %q, want turn-a", row.TurnID)
		}
	}
	turnB, _ := readAll(t, dir, Filter{TurnID: "turn-b"})
	if len(turnB) != 1 {
		t.Fatalf("turn-b filter: got %d rows, want 1", len(turnB))
	}
}

func TestACorruptLineInTheMiddleIsReportedAndSkipped(t *testing.T) {
	dir := t.TempDir()
	writer := NewWriter(dir)
	at := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		row := sampleRow("tool_gate", VerdictAsk, at.Add(time.Duration(i)*time.Minute))
		row.RequestID = fmt.Sprintf("or-req-%d", i)
		if _, err := writer.Append(row); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	path := filepath.Join(dir, "2026-09-18.jsonl")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	lines[2] = `{"id":"2026-09-18-broken","schema":1,"at":"2026-09-1`
	lines[3] = `{"id":"2026-09-18-future","schema":99,"point":"tool_gate"}`
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	rows, report := readAll(t, dir, Filter{})
	if len(rows) != 3 {
		t.Fatalf("the three readable rows must survive, got %d", len(rows))
	}
	if rows[2].RequestID != "or-req-4" {
		t.Errorf("the reader must go past the damage to the last row, it ended at %q", rows[2].RequestID)
	}
	if len(report.Corrupt) != 2 {
		t.Fatalf("2 unreadable lines must be reported, got %d: %+v", len(report.Corrupt), report.Corrupt)
	}
	if report.Corrupt[0].Line != 3 || report.Corrupt[1].Line != 4 {
		t.Errorf("the report must name the lines, got %d and %d", report.Corrupt[0].Line, report.Corrupt[1].Line)
	}
	if report.Corrupt[0].Err == nil || !strings.Contains(report.Corrupt[1].Err.Error(), "schema 99") {
		t.Errorf("the report must carry the cause, got %+v", report.Corrupt)
	}
	t.Logf("corrupt lines reported: %+v", report.Corrupt)
}

func TestARowWithAnUnknownVerdictIsCountedCorrupt(t *testing.T) {
	dir := t.TempDir()
	bad := `{"id":"2026-09-18-badverdict00000000000000000000","schema":1,"at":"2026-09-18T19:24:34.242Z","point":"tool_gate","questions":"tool_gate","version":1,"verdict":"maybe"}`
	writeLegacyLine(t, dir, bad)
	rows, report := readAll(t, dir, Filter{})
	if len(rows) != 0 {
		t.Fatalf("a row with an unknown verdict must not be read, got %+v", rows)
	}
	if len(report.Corrupt) != 1 || !strings.Contains(report.Corrupt[0].Err.Error(), "unknown verdict") {
		t.Fatalf("an unknown verdict must be reported corrupt, got %+v", report.Corrupt)
	}
}

func TestBackfillChangesOnlyTheOutcomeField(t *testing.T) {
	dir := t.TempDir()
	writer := NewWriter(dir)
	at := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	var ids []string
	for i := 0; i < 3; i++ {
		row := sampleRow("tool_gate", VerdictDeny, at.Add(time.Duration(i)*time.Minute))
		stored, err := writer.Append(row)
		if err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
		ids = append(ids, stored.ID)
	}

	before, _ := readAll(t, dir, Filter{})
	target := ids[1]

	outcome := Outcome{At: at.Add(time.Hour), Kind: "reverted", Detail: "the owner allowed it by hand"}
	if err := writer.Backfill(target, outcome); err != nil {
		t.Fatalf("Backfill: %v", err)
	}
	after, _ := readAll(t, dir, Filter{})

	if len(before) != 3 || len(after) != 3 {
		t.Fatalf("a backfill adds no row and removes none, %d before and %d after", len(before), len(after))
	}
	for i := range before {
		if before[i].Outcome != nil {
			t.Fatalf("row %d had an outcome before the backfill", i)
		}
		beforeBytes := canonicalWithTheOutcomeRemoved(t, before[i])
		afterBytes := canonicalWithTheOutcomeRemoved(t, after[i])
		if string(beforeBytes) != string(afterBytes) {
			t.Fatalf("row %d changed outside its outcome\nbefore %s\nafter  %s", i, beforeBytes, afterBytes)
		}
	}
	if after[0].Outcome != nil || after[2].Outcome != nil {
		t.Fatalf("the backfill reached a row it was not given")
	}
	if after[1].Outcome == nil {
		t.Fatal("the backfill did not attach anything")
	}
	if after[1].Outcome.Kind != "reverted" || after[1].Outcome.Detail != outcome.Detail {
		t.Fatalf("the outcome came back wrong: %+v", after[1].Outcome)
	}
	t.Logf("row with its outcome removed, identical before and after:\n%s", canonicalWithTheOutcomeRemoved(t, after[1]))
}

func TestTenThousandRowsReadBackThroughAFilterOnAFlatHeap(t *testing.T) {
	dir := t.TempDir()
	writer := NewWriter(dir)
	start := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	const rows = 10000
	writeStarted := time.Now()
	for i := 0; i < rows; i++ {
		point := "tool_gate"
		if i%2 == 1 {
			point = "recall_drop"
		}
		row := sampleRow(point, VerdictAsk, start.Add(time.Duration(i)*time.Minute))
		row.RequestID = fmt.Sprintf("or-req-%05d", i)
		if _, err := writer.Append(row); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	writeTook := time.Since(writeStarted)

	var bytesOnDisk int64
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			t.Fatalf("Info: %v", err)
		}
		bytesOnDisk += info.Size()
	}

	runtime.GC()
	var before, during, after runtime.MemStats
	runtime.ReadMemStats(&before)
	peak := before.HeapAlloc

	seen := 0
	readStarted := time.Now()
	report, err := NewReader(dir).Each(Filter{Point: "tool_gate"}, func(row Row) error {
		seen++
		if row.Point != "tool_gate" {
			return fmt.Errorf("the filter let %q through", row.Point)
		}
		if seen%500 == 0 {
			runtime.ReadMemStats(&during)
			if during.HeapAlloc > peak {
				peak = during.HeapAlloc
			}
		}
		return nil
	})
	readTook := time.Since(readStarted)
	if err != nil {
		t.Fatalf("Each: %v", err)
	}
	runtime.GC()
	runtime.ReadMemStats(&after)

	if seen != rows/2 {
		t.Fatalf("the filter matched %d rows, want %d", seen, rows/2)
	}
	if report.Scanned != rows || report.Matched != rows/2 {
		t.Fatalf("report scanned %d and matched %d, want %d and %d", report.Scanned, report.Matched, rows, rows/2)
	}
	if len(report.Corrupt) != 0 {
		t.Fatalf("a ledger this writer wrote has no corrupt line, got %+v", report.Corrupt)
	}

	growth := int64(peak) - int64(before.HeapAlloc)
	retained := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	t.Logf("%d rows over %d files, %d bytes on disk, write %s, read %s",
		rows, report.Files, bytesOnDisk, writeTook.Round(time.Millisecond), readTook.Round(time.Millisecond))
	t.Logf("heap before %d B, peak during %d B, peak growth %d B, retained after a collection %d B, allocated over the whole read %d B",
		before.HeapAlloc, peak, growth, retained, after.TotalAlloc-before.TotalAlloc)

	const peakCeiling = 4 << 20
	const retainedCeiling = 256 << 10
	if growth > peakCeiling {
		t.Fatalf("the reader grew the heap by %d B over %d B of ledger, the ceiling is %d B",
			growth, bytesOnDisk, peakCeiling)
	}
	if retained > retainedCeiling {
		t.Fatalf("the reader retained %d B after %d B of ledger, the ceiling is %d B",
			retained, bytesOnDisk, retainedCeiling)
	}
}

func TestSummaryCountsRowsAndDates(t *testing.T) {
	dir := t.TempDir()
	writer := NewWriter(dir)
	days := []time.Time{
		time.Date(2026, 9, 16, 9, 30, 0, 0, time.UTC),
		time.Date(2026, 9, 17, 9, 30, 0, 0, time.UTC),
		time.Date(2026, 9, 18, 9, 30, 0, 0, time.UTC),
	}
	for _, day := range days {
		for i := 0; i < 4; i++ {
			if _, err := writer.Append(sampleRow("tool_gate", VerdictAsk, day.Add(time.Duration(i)*time.Minute))); err != nil {
				t.Fatalf("Append: %v", err)
			}
		}
	}
	stats, err := Summary(dir)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if stats.Rows != 12 || stats.Days != 3 {
		t.Fatalf("Summary counted %d rows over %d days, want 12 over 3", stats.Rows, stats.Days)
	}
	if stats.First.UTC().Format(dayLayout) != "2026-09-16" || stats.Last.UTC().Format(dayLayout) != "2026-09-18" {
		t.Fatalf("date range %s to %s, want 2026-09-16 to 2026-09-18",
			stats.First.UTC().Format(dayLayout), stats.Last.UTC().Format(dayLayout))
	}
	want := "12 rows over 3 days, 2026-09-16 to 2026-09-18"
	if stats.String() != want {
		t.Fatalf("Stats.String() = %q, want %q", stats.String(), want)
	}
	t.Logf("tofu doctor line: ledger: %s", stats)
}

func TestTheLineTofuDoctorPrintsForTheProjectLedger(t *testing.T) {
	t.Chdir(t.TempDir())
	dir, err := sys.LogDir()
	if err != nil {
		t.Fatalf("LogDir: %v", err)
	}

	empty, err := Summary(dir)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	t.Logf("ledger: %s", empty)

	writer := NewWriter(dir)
	for day := 16; day <= 18; day++ {
		at := time.Date(2026, 9, day, 10, 0, 0, 0, time.UTC)
		for i := 0; i < 3; i++ {
			if _, err := writer.Append(sampleRow("tool_gate", VerdictAsk, at.Add(time.Duration(i)*time.Minute))); err != nil {
				t.Fatalf("Append: %v", err)
			}
		}
	}

	stats, err := Summary(dir)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if !strings.HasSuffix(filepath.ToSlash(dir), ".tofu/log") {
		t.Fatalf("the ledger lives at .tofu/log, LogDir returned %s", dir)
	}
	if stats.Rows != 9 {
		t.Fatalf("the doctor line must count 9 rows, it counted %d", stats.Rows)
	}
	line := fmt.Sprintf("ledger: %s", stats)
	want := "ledger: 9 rows over 3 days, 2026-09-16 to 2026-09-18"
	if line != want {
		t.Fatalf("doctor line %q, want %q", line, want)
	}
	t.Logf("%s", line)
}

func TestSummaryOfAnEmptyLedger(t *testing.T) {
	stats, err := Summary(filepath.Join(t.TempDir(), "log"))
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if stats.Rows != 0 || stats.String() != "empty" {
		t.Fatalf("an unused ledger reports %q with %d rows", stats.String(), stats.Rows)
	}
}
