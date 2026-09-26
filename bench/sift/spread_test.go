package sift

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"testing"

	"tofu/internal/sys"
)

const (
	sittingRuns       = 3
	recordedFigureDay = "2026-09-21"
	recordedBuild     = "typesafe/jev-1.13-20260917"
)

func TestTheLedgerBuildOnTheDayTheOldFigureWasRecorded(t *testing.T) {
	days, err := ReadLedgerBuilds(sys.RecordedStateDir("log"))
	if err != nil {
		t.Fatalf("ReadLedgerBuilds: %v", err)
	}
	for _, day := range slices.Sorted(maps.Keys(days)) {
		t.Logf("%s: %v", day, days[day])
	}
	recorded := days[recordedFigureDay]
	if len(recorded) != 1 || recorded[recordedBuild] == 0 {
		t.Fatalf("%s carries %v, and the reports of that day all name %s alone", recordedFigureDay, recorded, recordedBuild)
	}
}

type sitting struct {
	kept      int
	before    int
	after     int
	calls     int
	errors    int
	cost      float64
	scoreSum  float64
	scoreN    int
	builds    map[string]int
	stateSums []string
}

func (s sitting) saved() float64 { return 100 * float64(s.before-s.after) / float64(s.before) }

func TestTheJudgedArmThreeTimesInOneSitting(t *testing.T) {
	asker := liveClient(t)
	rule := shippedRule(t)
	all := corpusRows(t)

	var rows []Row
	skipped := 0
	for i, row := range all {
		if row.Task == "" || row.Command == "" || row.Output == "" {
			skipped++
			t.Logf("row %d skipped, session %q, command %q: a field the sift needs is empty", i, row.Session, row.Command)
			continue
		}
		rows = append(rows, row)
	}
	t.Logf("%d rows scored, %d skipped for a missing field, keep_at %.2f, no call discarded as a warm up", len(rows), skipped, rule.KeepAt)

	scores := map[string][]float64{}
	var runs []sitting
	for run := 1; run <= sittingRuns; run++ {
		one := sitting{builds: map[string]int{}}
		for i, row := range rows {
			planted, err := Plant(row, i)
			if err != nil {
				t.Fatalf("Plant: %v", err)
			}
			asked := asker.Ask(context.Background(), planted.Shell, planted.Units, row.Task)
			reading := Read(row, planted, asked.Cut(rule.KeepAt))
			if reading.NeedleKept {
				one.kept++
			}
			one.before += reading.Before
			one.after += reading.After
			one.calls += len(asked.Scores) + asked.Errors
			one.errors += asked.Errors
			one.cost += asked.Cost
			one.stateSums = append(one.stateSums, asked.StateSum)
			for build, n := range asked.Builds {
				one.builds[build] += n
			}
			for unit, score := range asked.Scores {
				at := fmt.Sprintf("row %d unit %d", i, unit)
				scores[at] = append(scores[at], score)
				one.scoreSum += score
				one.scoreN++
			}
		}
		runs = append(runs, one)
		t.Logf("run %d of %d: %d of %d needles kept, %d bytes to %d, %.1f%% saved, %d calls, %d failed, $%.5f, mean still_needed %.3f, builds %v",
			run, sittingRuns, one.kept, len(rows), one.before, one.after, one.saved(),
			one.calls, one.errors, one.cost, one.scoreSum/float64(one.scoreN), one.builds)
	}

	needles := make([]int, 0, sittingRuns)
	savings := make([]float64, 0, sittingRuns)
	spent := 0.0
	for i, one := range runs {
		needles = append(needles, one.kept)
		savings = append(savings, one.saved())
		spent += one.cost
		if !slices.Equal(runs[0].stateSums, one.stateSums) {
			t.Fatalf("run 1 and run %d put different states on the wire, so nothing below is a rerun spread", i+1)
		}
	}
	t.Logf("spread over %d runs in one sitting: needles kept %d to %d of %d, bytes saved %.1f%% to %.1f%%, $%.5f spent",
		sittingRuns, slices.Min(needles), slices.Max(needles), len(rows), slices.Min(savings), slices.Max(savings), spent)
	t.Logf("every run asked the same %d states, first digest %.12s", len(runs[0].stateSums), runs[0].stateSums[0])

	identical, straddled, partial := 0, 0, 0
	sum, widest, widestAt := 0.0, 0.0, ""
	for at, values := range scores {
		if len(values) != sittingRuns {
			partial++
			continue
		}
		low, high := slices.Min(values), slices.Max(values)
		sum += high - low
		if high == low {
			identical++
		}
		if low < rule.KeepAt && high >= rule.KeepAt {
			straddled++
		}
		if high-low > widest {
			widest, widestAt = high-low, at
		}
	}
	answered := len(scores) - partial
	t.Logf("per unit: %d answered in all %d runs, %d gave the identical score every time, %d straddle keep_at %.2f, mean spread %.3f, widest %.2f at %s, %d answered in fewer than %d runs",
		answered, sittingRuns, identical, straddled, rule.KeepAt, sum/float64(answered), widest, widestAt, partial, sittingRuns)
}
