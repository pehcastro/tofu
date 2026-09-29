package airbnb

import (
	"fmt"
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
	MainTokens    int
	BrowserTokens int
	TabsOpened    int
	Repeated      int
	Refused       int
}

func Score(task Task, run Run) Row {
	row := Row{Arm: run.Arm, Conditions: run.Conditions, Wall: time.Duration(run.WallMS) * time.Millisecond, MainTokens: run.MainTokens, BrowserTokens: run.BrowserTokens, TabsOpened: len(run.Tabs), Repeated: run.Repeated, Refused: run.Refused}
	for _, step := range task.Steps {
		passed := passes(step.Check, run)
		if passed {
			row.Passed++
		}
		row.Steps = append(row.Steps, Result{Step: step.Step, Passed: passed})
	}
	return row
}

func Render(rows []Row) (string, error) {
	var table strings.Builder
	table.WriteString("| arm | steps | wall | main tokens | browser tokens | tabs opened | repeated | refused | failed steps | main build | browser build | credential | wire | machine | date |\n")
	table.WriteString("|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, row := range rows {
		if row.Conditions.Credential != rows[0].Conditions.Credential {
			return "", fmt.Errorf("arm %s ran on a %s credential and arm %s on %s, and the two are not compared", row.Arm, row.Conditions.Credential, rows[0].Arm, rows[0].Conditions.Credential)
		}
		var failed []string
		for _, result := range row.Steps {
			if !result.Passed {
				failed = append(failed, fmt.Sprint(result.Step))
			}
		}
		conditions := row.Conditions
		fmt.Fprintf(&table, "| %s | %d of %d | %.0f s | %d | %d | %d | %d | %d | %s | %s | %s | %s | %s | %s | %s |\n",
			row.Arm, row.Passed, len(row.Steps), row.Wall.Seconds(), row.MainTokens, row.BrowserTokens, row.TabsOpened, row.Repeated, row.Refused,
			strings.Join(failed, " "), conditions.MainBuild, conditions.BrowserBuild, conditions.Credential, conditions.Wire, conditions.Machine, conditions.Date)
	}
	return table.String(), nil
}
