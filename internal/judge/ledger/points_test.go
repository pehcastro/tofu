package ledger

import (
	"reflect"
	"testing"
	"time"
)

func TestPointsSummariseEachPointInsideTheWindowByLocalDay(t *testing.T) {
	zone := time.FixedZone("UTC-3", -3*60*60)
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, zone)
	dir := t.TempDir()
	writer := NewWriterWithClock(dir, func() time.Time { return now })
	reason := func(mode Mode, comparison string, threshold float64) *Reason {
		return &Reason{Question: "risk", Comparison: comparison, Threshold: threshold, Mode: mode}
	}
	write := func(row Row) Row {
		t.Helper()
		written, err := writer.Append(row)
		if err != nil {
			t.Fatal(err)
		}
		return written
	}
	write(Row{Point: "tool_gate", At: now.AddDate(0, 0, -10), Verdict: VerdictAsk, Reason: reason(ModeShadow, "risk_ask_at", 9)})
	write(Row{Point: "tool_gate", At: time.Date(2026, 10, 6, 21, 30, 0, 0, zone), Verdict: VerdictAsk, LatencyMS: 100, Cost: 0.001, Reason: reason(ModeShadow, "risk_ask_at", 1.5)})
	write(Row{Point: "tool_gate", At: now.Add(-time.Hour), Verdict: VerdictAllow, LatencyMS: 300, Cost: 0.002, Reason: reason(ModeEnforced, "risk_ask_at", 2)})
	write(Row{Point: "tool_gate", At: now.Add(-3 * time.Hour), Verdict: VerdictDeny, LatencyMS: 200, Reason: reason(ModeShadow, "risk_deny_at", 2.5)})
	agreed := write(Row{Point: "stop_check", At: now.Add(-30 * time.Minute), Verdict: VerdictAllow, Reason: reason(ModeShadow, "done_at", 0.5)})
	answered := write(Row{Point: "stop_check", At: now.Add(-20 * time.Minute), Verdict: VerdictAsk, Reason: reason(ModeShadow, "done_at", 0.5)})
	scoped := write(Row{Point: "memory_scope", At: now.Add(-10 * time.Minute)})
	for id, outcome := range map[string]Outcome{
		agreed.ID:   {Kind: "hand-labeled", Detail: "allow"},
		answered.ID: {Kind: "gate-answer", Detail: "allow"},
		scoped.ID:   {Kind: "memory-scope", Detail: "project"},
	} {
		if err := writer.Backfill(id, outcome); err != nil {
			t.Fatal(err)
		}
	}

	points, report, err := NewReader(dir).Points(now.AddDate(0, 0, -7), now)
	if err != nil || len(report.Corrupt) > 0 {
		t.Fatalf("points: %v, corrupt %v", err, report.Corrupt)
	}
	want := []PointSummary{
		{Point: "memory_scope", Mode: ModeUnknown, Count: 1, Week: [7]int{0, 0, 0, 0, 0, 0, 1}, Thresholds: []Threshold{}},
		{Point: "stop_check", Mode: ModeShadow, Count: 2, Week: [7]int{0, 0, 0, 0, 0, 0, 2}, WouldAsk: 1, Labeled: 2, Agreed: 1,
			Thresholds: []Threshold{{Comparison: "done_at", Value: 0.5}}},
		{Point: "tool_gate", Mode: ModeEnforced, Count: 3, Week: [7]int{0, 0, 0, 0, 1, 0, 2}, WouldAsk: 2, MeanMS: 200, CostUSD: 0.003,
			Thresholds: []Threshold{{Comparison: "risk_ask_at", Value: 2}, {Comparison: "risk_deny_at", Value: 2.5}}},
	}
	if !reflect.DeepEqual(points, want) {
		t.Fatalf("points\n got %+v\nwant %+v", points, want)
	}
}
