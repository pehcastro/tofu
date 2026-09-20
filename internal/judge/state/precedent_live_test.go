package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"tofu/internal/judge/ledger"
	"tofu/internal/konst"
)

const realLedgerDir = "../../../.tofu/log"

type recordedCall struct {
	Tool  string         `json:"tool"`
	Input map[string]any `json:"input"`
	Cwd   string         `json:"cwd"`
}

func fingerprintedCopyOfTheRealLedger(t *testing.T) (dir string, rows []ledger.Row, skippedWithoutState int) {
	t.Helper()
	if _, err := os.Stat(realLedgerDir); err != nil {
		t.Skipf("skipped, not counted as a pass: this machine has no ledger at %s (%v)", realLedgerDir, err)
	}
	report, err := ledger.NewReader(realLedgerDir).Each(ledger.Filter{Point: ToolGatePoint}, func(row ledger.Row) error {
		if len(row.State) == 0 {
			skippedWithoutState++
			return nil
		}
		var call recordedCall
		if err := json.Unmarshal(row.State, &call); err != nil {
			return fmt.Errorf("row %s carries a state body this builder cannot read: %w", row.ID, err)
		}
		row.Fingerprint = FingerprintOf(ToolGateInput{Tool: call.Tool, Input: call.Input, Cwd: call.Cwd, ProjectDir: call.Cwd})
		row.Schema = ledger.FingerprintSchema
		rows = append(rows, row)
		return nil
	})
	if err != nil {
		t.Fatalf("reading the real ledger: %v", err)
	}
	if len(rows) == 0 {
		t.Fatalf("%s scanned %d rows and none carried a state body, so this proves nothing", realLedgerDir, report.Scanned)
	}

	dir = t.TempDir()
	byDay := map[string][]byte{}
	for _, row := range rows {
		line, err := ledger.Canonical(row)
		if err != nil {
			t.Fatalf("canonical %s: %v", row.ID, err)
		}
		byDay[row.Day()] = append(append(byDay[row.Day()], line...), '\n')
	}
	for day, body := range byDay {
		if err := os.WriteFile(filepath.Join(dir, day+".jsonl"), body, 0o644); err != nil {
			t.Fatalf("writing the copy: %v", err)
		}
	}
	return dir, rows, skippedWithoutState
}

func TestTheRealLedgerGroupsIntoFingerprints(t *testing.T) {
	_, rows, skipped := fingerprintedCopyOfTheRealLedger(t)
	groups := map[string][]string{}
	for _, row := range rows {
		groups[row.Fingerprint] = append(groups[row.Fingerprint], row.ID)
	}
	if len(groups) >= len(rows) {
		t.Fatalf("%d rows produced %d fingerprints, so nothing was recognised as a repeat", len(rows), len(groups))
	}
	if len(groups) < 2 {
		t.Fatalf("%d rows produced %d fingerprint, so everything collapsed into one bucket", len(rows), len(groups))
	}
	sizes := make([]int, 0, len(groups))
	for _, ids := range groups {
		sizes = append(sizes, len(ids))
	}
	sort.Sort(sort.Reverse(sort.IntSlice(sizes)))
	t.Logf("%d tool_gate rows with a state body, %d skipped for an elided state, %d distinct fingerprints, largest groups %v",
		len(rows), skipped, len(groups), sizes[:min(5, len(sizes))])
}

func TestAPrecedentQueryOverTheRealLedgerReadsLikeSomethingAPersonWrote(t *testing.T) {
	dir, rows, _ := fingerprintedCopyOfTheRealLedger(t)
	target := rows[len(rows)-1]
	found, err := ledger.NewReader(dir).Precedents(target)
	if err != nil {
		t.Fatalf("precedents: %v", err)
	}
	if len(found) == 0 {
		t.Fatalf("the last row of the real ledger found no precedent among %d earlier rows", len(rows)-1)
	}
	t.Log(renderPrecedents(target, found))

	passedTheFilter, sameCall := 0, 0
	for _, row := range rows[:len(rows)-1] {
		distance, comparable := ledger.AnswerDistance(target.Answers, row.Answers)
		if row.Fingerprint == target.Fingerprint {
			sameCall++
			passedTheFilter++
			continue
		}
		if comparable && distance <= 2*konst.ThresholdDeadBand {
			passedTheFilter++
		}
	}
	t.Logf("%d of %d earlier rows passed the filter before the shortlist cut it to %d, and %d of those are the same call",
		passedTheFilter, len(rows)-1, len(found), sameCall)
}

func renderPrecedents(target ledger.Row, found []ledger.Precedent) string {
	out := fmt.Sprintf("%s  %s  %s  verdict %s\n  fingerprint %s\n  %s\n\n  nearest precedents, %d of them:\n",
		target.ID, target.Point, target.At.Format(time.RFC3339), target.Verdict, target.Fingerprint, callOf(target), len(found))
	for _, one := range found {
		why := fmt.Sprintf("answers %.4f apart", one.Distance)
		if !one.Comparable {
			why = "no question in common"
		}
		if one.SameFingerprint {
			why = "the same call, " + why
		}
		outcome := ""
		if one.Row.Outcome != nil {
			outcome = ", outcome " + one.Row.Outcome.Kind
		}
		out += fmt.Sprintf("    %s  %s ago  verdict %s%s\n      %s\n      %s\n",
			one.Row.ID, target.At.Sub(one.Row.At).Round(time.Second), one.Row.Verdict, outcome, why, callOf(one.Row))
	}
	return out
}

func callOf(row ledger.Row) string {
	var call recordedCall
	if err := json.Unmarshal(row.State, &call); err != nil {
		return "state unreadable"
	}
	if command, ok := call.Input["command"].(string); ok {
		return call.Tool + " " + command
	}
	shaped, err := json.Marshal(call.Input)
	if err != nil {
		return call.Tool
	}
	return call.Tool + " " + string(shaped)
}
