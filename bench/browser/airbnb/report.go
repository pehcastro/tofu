package airbnb

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
)

type Result struct {
	Step   int
	Passed bool
}

type Row struct {
	Arm           Arm
	Conditions    Conditions
	Steps         []Result
	Passed        int
	Wall          time.Duration
	Forks         int
	MainTokens    int
	BrowserTokens *int
	TabsOpened    int
	Repeated      int
	Refused       int
	Mixed         bool
	TabsClosed    int
}

func Score(task Task, run Run) Row {
	row := Row{Arm: run.Arm, Conditions: run.Conditions, Wall: time.Duration(run.WallMS) * time.Millisecond, Forks: run.Forks, MainTokens: run.MainTokens, BrowserTokens: run.BrowserTokens, TabsOpened: len(run.Tabs), Repeated: run.Repeated, Refused: run.Refused, Mixed: run.Mixed, TabsClosed: run.TabsClosed}
	for _, step := range task.Steps {
		passed := passes(step.Check, run)
		if passed {
			row.Passed++
		}
		row.Steps = append(row.Steps, Result{Step: step.Step, Passed: passed})
	}
	return row
}

func (r Row) TotalTokens() (int, bool) {
	if r.BrowserTokens == nil {
		return 0, false
	}
	return r.MainTokens + *r.BrowserTokens, true
}

func Render(rows []Row) (string, error) {
	sorted := slices.Clone(rows)
	unknownLast := func(row Row) int {
		if total, known := row.TotalTokens(); known {
			return total
		}
		return math.MaxInt
	}
	slices.SortStableFunc(sorted, func(a, b Row) int {
		return cmp.Or(cmp.Compare(b.Passed, a.Passed), cmp.Compare(unknownLast(a), unknownLast(b)))
	})
	var table strings.Builder
	table.WriteString("| arm | mode | steps | wall | total tokens | usd | forks | main tokens | browser tokens | tabs closed before | tabs opened | repeated | refused | failed steps | main build | browser build | main credential | browser credential | wire | machine | date |\n")
	table.WriteString("|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, row := range sorted {
		if row.Conditions.Credential != rows[0].Conditions.Credential {
			return "", fmt.Errorf("the main model of arm %s ran on a %s credential and that of arm %s on %s, and the two are not compared", row.Arm, row.Conditions.Credential, rows[0].Arm, rows[0].Conditions.Credential)
		}
		var failed []string
		for _, result := range row.Steps {
			if !result.Passed {
				failed = append(failed, fmt.Sprint(result.Step))
			}
		}
		browserTokens, totalTokens := "not recorded", "not recorded"
		if total, known := row.TotalTokens(); known {
			browserTokens, totalTokens = fmt.Sprint(*row.BrowserTokens), fmt.Sprint(total)
		}
		conditions, mode := row.Conditions, "as set"
		if row.Mixed {
			mode = "mixed"
		}
		fmt.Fprintf(&table, "| %s | %s | %d of %d | %.0f s | %s | - | %d | %d | %s | %d | %d | %d | %d | %s | %s | %s | %s | %s | %s | %s | %s |\n",
			row.Arm, mode, row.Passed, len(row.Steps), row.Wall.Seconds(), totalTokens, row.Forks, row.MainTokens, browserTokens, row.TabsClosed, row.TabsOpened, row.Repeated, row.Refused,
			strings.Join(failed, " "), conditions.MainBuild, conditions.BrowserBuild, conditions.Credential, conditions.BrowserCredential, conditions.Wire, conditions.Machine, conditions.Date)
	}
	return table.String(), nil
}
