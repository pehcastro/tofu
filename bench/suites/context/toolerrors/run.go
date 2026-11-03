package toolerrors

import (
	"sort"
	"time"

	"tofu/bench/corpus"
)

const minCallsForWorst = 5

type FailingCall struct {
	Turn     string
	Tool     string
	Command  string
	Error    string
	ExitCode *int
	Category Category
}

type ToolRow struct {
	Tool       string
	Calls      int
	Failures   int
	ByCategory map[Category]int
	Example    FailingCall
}

func (r ToolRow) Rate() float64 {
	if r.Calls == 0 {
		return 0
	}
	return float64(r.Failures) / float64(r.Calls)
}

type Result struct {
	SessionsDir string
	ReadAt      time.Time
	Entries     int
	Sessions    int
	Skips       []corpus.SkippedTurn
	Calls       int
	Failures    int
	Tools       []ToolRow
	ByCategory  map[Category]int
	Worst       ToolRow
	WorstFound  bool

	RecoveredFromBareExitCode FailingCall
	RecoveredFound            bool

	OldestTurn      corpus.Turn
	OldestTurnFound bool
}

func Run(sessionsDir string) (Result, error) {
	walked, err := corpus.WalkSessions(sessionsDir)
	if err != nil {
		return Result{}, err
	}
	result := Result{
		SessionsDir: sessionsDir,
		ReadAt:      time.Now(),
		Entries:     walked.EntryCount,
		Sessions:    len(walked.Turns),
		Skips:       walked.Skipped,
		ByCategory:  map[Category]int{},
	}
	if len(walked.Turns) > 0 {
		result.OldestTurn = walked.Turns[0]
		result.OldestTurnFound = true
	}
	rows := map[string]*ToolRow{}
	for _, turn := range walked.Turns {
		for _, step := range turn.Steps {
			for _, call := range step.ToolCalls {
				row, ok := rows[call.Tool]
				if !ok {
					row = &ToolRow{Tool: call.Tool, ByCategory: map[Category]int{}}
					rows[call.Tool] = row
				}
				row.Calls++
				result.Calls++
				if !Failed(call) {
					continue
				}
				category := Classify(call)
				failing := FailingCall{
					Turn: turn.ID, Tool: call.Tool, Command: call.Command,
					Error: call.Error, ExitCode: call.ExitCode, Category: category,
				}
				if row.Failures == 0 {
					row.Example = failing
				}
				if !result.RecoveredFound && call.Error == "" && category != Unknown {
					result.RecoveredFromBareExitCode = failing
					result.RecoveredFound = true
				}
				row.Failures++
				row.ByCategory[category]++
				result.Failures++
				result.ByCategory[category]++
			}
		}
	}
	for _, row := range rows {
		result.Tools = append(result.Tools, *row)
	}
	sort.Slice(result.Tools, func(i, j int) bool { return result.Tools[i].Tool < result.Tools[j].Tool })

	for _, row := range result.Tools {
		if row.Calls < minCallsForWorst {
			continue
		}
		if !result.WorstFound || row.Rate() > result.Worst.Rate() {
			result.Worst = row
			result.WorstFound = true
		}
	}
	return result, nil
}
