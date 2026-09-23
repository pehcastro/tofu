package method_test

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"tofu/internal/judge/ledger"
	"tofu/internal/judge/method"
)

const recordedLedger = "../../../.tofu/log"

func TestTheStopCheckCostTheTableStatesIsTheOneTheRecordedLedgerCarries(t *testing.T) {
	if _, err := os.Stat(recordedLedger); err != nil {
		t.Skipf("skipped, and counted: no recorded ledger at %s: %v", recordedLedger, err)
	}
	perSession := map[string]float64{}
	report, err := ledger.NewReader(recordedLedger).Each(
		ledger.Filter{Point: "stop_check", Origin: ledger.OriginTurn},
		func(row ledger.Row) error {
			perSession[row.TurnID] += row.Cost
			return nil
		})
	if err != nil {
		t.Fatalf("reading the recorded ledger: %v", err)
	}
	if len(perSession) == 0 {
		t.Fatal("the recorded ledger carries no stop_check row, so the table's cost line cannot be checked")
	}
	spent := make([]float64, 0, len(perSession))
	total := 0.0
	for _, one := range perSession {
		spent = append(spent, one)
		total += one
	}
	sort.Float64s(spent)
	median := spent[len(spent)/2]
	t.Logf("ledger: %d files, %d rows scanned, %d stop_check rows over %d sessions, median $%.6f, worst $%.6f, $%.4f in total, %d corrupt",
		report.Files, report.Scanned, report.Matched, len(spent), median, spent[len(spent)-1], total, len(report.Corrupt))

	stopCheck, err := method.Load(os.DirFS(libraryDir))
	if err != nil {
		t.Fatalf("loading the shipped method table: %v", err)
	}
	chosen, err := stopCheck.Of("stop_check")
	if err != nil {
		t.Fatalf("%v", err)
	}
	if !strings.Contains(chosen.Cost, fmt.Sprintf("$%.6f", median)) {
		t.Fatalf("the table states %q and the recorded ledger says a median $%.6f a session", chosen.Cost, median)
	}
}
