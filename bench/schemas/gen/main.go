package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"tofu/bench/report"
	"tofu/bench/schemas"
)

func main() {
	if err := generate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func generate() error {
	sessionsDir := filepath.Join("..", "..", "..", ".tofu", "sessions")
	usable, _, err := schemas.ReadSessions(sessionsDir)
	if err != nil {
		return err
	}
	var target schemas.TurnUsage
	for _, turn := range usable {
		if turn.ID == schemas.TargetTurnID {
			target = turn
		}
	}
	if len(target.Steps) == 0 {
		return fmt.Errorf("bench/schemas/gen: %s is not in %s", schemas.TargetTurnID, sessionsDir)
	}
	defs := schemas.FullRegistry(".").Definitions()
	whole := schemas.Measure(defs)
	cohort, err := schemas.BuildCohort(sessionsDir)
	if err != nil {
		return err
	}
	cohort.Dir = ".tofu/sessions"
	names := target.CalledToolNames()
	deferred := schemas.CostOfDeferring(whole, names, schemas.FullToolCount)
	missing, _ := schemas.Diff(names, schemas.Names(defs))
	rendered := schemas.Report{
		Date:                time.Now().Format("2006-01-02"),
		Machine:             hostname(),
		BuildNote:           "tofu's own claude-sub credential, wire anthropic",
		Whole:               whole,
		ToolCount:           schemas.FullToolCount,
		Target:              target,
		TargetNames:         names,
		Cohort:              cohort,
		Deferred:            deferred,
		CalledNotInRegistry: missing,
	}.Render()
	path := filepath.Join("..", fmt.Sprintf("report-%s.md", time.Now().Format("2006-01-02")))
	if err := report.Write(path, []byte(rendered), 0o644, "schemas report"); err != nil {
		return err
	}
	fmt.Println(path, "written")
	return nil
}

func hostname() string {
	name, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return name
}
