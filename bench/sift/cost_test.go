package sift

import (
	"os"
	"path/filepath"
	"testing"

	"tofu/bench/stat"
	"tofu/internal/konst"
	"tofu/internal/sift"
)

const (
	ledgerDir  = "../../.tofu/log"
	sessionDir = "../../.tofu/sessions"

	dollarsPerShellResultTOFU217 = 0.00027
	millisPerShellResultTOFU217  = 838.0
	bytesSavedFractionTOFU217    = 0.540

	sessionFloor = 100
)

func TestReadLedgerSeesAShellSiftRowWhenThereIsOne(t *testing.T) {
	dir := t.TempDir()
	rows := `{"id":"a","point":"shell_sift@1","latency_ms":900,"cost":0.0000303}
{"id":"b","point":"tool_gate","latency_ms":0,"cost":0}
{"id":"c","blocked":true,"rule_id":"em_dash"}
`
	if err := os.WriteFile(filepath.Join(dir, "2026-09-22.jsonl"), []byte(rows), 0o600); err != nil {
		t.Fatal(err)
	}
	read, err := ReadLedger(dir)
	if err != nil {
		t.Fatalf("ReadLedger: %v", err)
	}
	if read.Rows != 3 || read.ShellSiftDecided != 1 || read.SkippedNoWire != 1 || read.SkippedNoPoint != 1 {
		t.Fatalf("%d rows, %d shell_sift, %d with no wire call, %d with no point, want 3, 1, 1 and 1",
			read.Rows, read.ShellSiftDecided, read.SkippedNoWire, read.SkippedNoPoint)
	}
	if len(read.Millis) != 1 || read.Millis[0] != 900 {
		t.Fatalf("wire calls %v, want one at 900 ms", read.Millis)
	}
}

func TestWhatSwitchingTheShellSiftOnWouldCost(t *testing.T) {
	if _, err := os.Stat(sessionDir); err != nil {
		t.Skipf("no recorded sessions at %s: %v", sessionDir, err)
	}
	read, err := ReadSessionShells(sessionDir)
	if err != nil {
		t.Fatalf("ReadSessionShells: %v", err)
	}
	if len(read.Sessions) < sessionFloor {
		t.Fatalf("%d recorded sessions, too few to state a median over", len(read.Sessions))
	}
	logRead, err := ReadLedger(ledgerDir)
	if err != nil {
		t.Fatalf("ReadLedger: %v", err)
	}
	if logRead.ShellSiftDecided != 0 {
		t.Fatalf("the ledger carries %d shell_sift rows, so this report must measure them rather than derive from TOFU-217", logRead.ShellSiftDecided)
	}

	t.Logf("ledger: %d files, %d rows, %d wire calls at %v, %d skipped for no point, %d skipped for no wire call, 0 of them shell_sift",
		logRead.Files, logRead.Rows, len(logRead.Millis), logRead.PointsWithCalls, logRead.SkippedNoPoint, logRead.SkippedNoWire)

	_, worstMillis := stat.Spread(logRead.Millis)
	t.Logf("recorded jev call: p50 %.0f ms, p95 %.0f ms, worst %.0f ms, p50 $%.7f, p95 $%.7f",
		stat.Median(logRead.Millis), stat.Percentile(logRead.Millis, 95), worstMillis,
		stat.Median(logRead.Costs), stat.Percentile(logRead.Costs, 95))

	for _, skipped := range read.Skipped {
		t.Logf("session skipped: %s, %s", skipped.Path, skipped.Reason)
	}
	shells := make([]float64, 0, len(read.Sessions))
	for _, session := range read.Sessions {
		shells = append(shells, float64(session.Shells))
	}
	_, worstShells := stat.Spread(shells)
	t.Logf("sessions: %d read, %d skipped, shell results per session p50 %.0f, p95 %.0f, worst %.0f, %d in total",
		len(read.Sessions), len(read.Skipped), stat.Median(shells), stat.Percentile(shells, 95), worstShells, read.Totals.Shells)

	var running []float64
	for _, count := range shells {
		if count > 0 {
			running = append(running, count)
		}
	}
	t.Logf("sessions that ran a shell at all: %d of %d, p50 %.0f, p95 %.0f",
		len(running), len(shells), stat.Median(running), stat.Percentile(running, 95))

	rows := corpusRows(t)
	candidates := 0
	for i, row := range rows {
		planted, err := Plant(row, i)
		if err != nil {
			t.Fatalf("Plant: %v", err)
		}
		for _, unit := range planted.Units {
			if unit.Held == sift.NotHeld {
				candidates++
			}
		}
	}
	perShell := float64(candidates) / float64(len(rows))
	t.Logf("corpus: %.1f candidate units per shell result, %d over %d rows", perShell, candidates, len(rows))
	t.Logf("cost of one judged sift: $%.6f from the TOFU-217 log, $%.6f from the ledger p50 times candidates",
		dollarsPerShellResultTOFU217, perShell*stat.Median(logRead.Costs))

	dollars := make([]float64, len(shells))
	spent := 0.0
	for i, count := range shells {
		dollars[i] = count * dollarsPerShellResultTOFU217
		spent += dollars[i]
	}
	_, worstDollars := stat.Spread(dollars)
	t.Logf("dollars per session: p50 $%.6f, p95 $%.6f, worst $%.6f, $%.4f over all %d sessions",
		stat.Median(dollars), stat.Percentile(dollars, 95), worstDollars, spent, len(dollars))

	totals := read.Totals
	t.Logf("bytes: shell results are %.0f of %.0f tool bytes once, %.1f%%, and %.0f of %.0f counting every resend, %.1f%%",
		totals.ShellBytes, totals.ToolBytes, 100*totals.ShellBytes/totals.ToolBytes,
		totals.ShellReads, totals.ToolReads, 100*totals.ShellReads/totals.ToolReads)
	t.Logf("bytes not sent: %.1f%% of shell output, %.1f%% of everything the model reads from tools once, %.1f%% counting every resend",
		100*bytesSavedFractionTOFU217,
		100*bytesSavedFractionTOFU217*totals.ShellBytes/totals.ToolBytes,
		100*bytesSavedFractionTOFU217*totals.ShellReads/totals.ToolReads)

	batches := 1.0
	for batches*konst.SiftConcurrency < perShell {
		batches++
	}
	t.Logf("latency added per shell tool call: %.0f ms from the TOFU-217 log, %.0f ms if the ledger p95 ran %.1f deep at concurrency %d",
		millisPerShellResultTOFU217, stat.Percentile(logRead.Millis, 95)*batches, perShell, konst.SiftConcurrency)
}
