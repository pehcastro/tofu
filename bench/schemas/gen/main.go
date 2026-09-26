package main

import (
	"fmt"
	"os"

	"tofu/bench/report"
	"tofu/bench/schemas"
	"tofu/internal/sys"
)

func main() {
	err := report.Generate("schemas report", func(machine, date string) (string, error) {
		sessionsDir := sys.RecordedStateDir("sessions")
		usable, _, err := schemas.ReadSessions(sessionsDir)
		if err != nil {
			return "", err
		}
		var target schemas.TurnUsage
		for _, turn := range usable {
			if turn.ID == schemas.TargetTurnID {
				target = turn
			}
		}
		if len(target.Steps) == 0 {
			return "", fmt.Errorf("bench/schemas/gen: %s is not in %s", schemas.TargetTurnID, sessionsDir)
		}
		defs := schemas.FullRegistry(".").Definitions()
		whole := schemas.Measure(defs)
		cohort, err := schemas.BuildCohort(sessionsDir)
		if err != nil {
			return "", err
		}
		cohort.Dir = ".tofu/sessions"
		names := target.CalledToolNames()
		deferred := schemas.CostOfDeferring(whole, names, schemas.FullToolCount)
		missing, _ := schemas.Diff(names, schemas.Names(defs))
		return schemas.Report{
			Date:                date,
			Machine:             machine,
			BuildNote:           "tofu's own claude-sub credential, wire anthropic",
			Whole:               whole,
			ToolCount:           schemas.FullToolCount,
			Target:              target,
			TargetNames:         names,
			Cohort:              cohort,
			Deferred:            deferred,
			CalledNotInRegistry: missing,
		}.Render(), nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
