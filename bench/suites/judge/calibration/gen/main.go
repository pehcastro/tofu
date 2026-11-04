package main

import (
	"fmt"
	"os"
	"time"

	"tofu/bench/kit/report"
	"tofu/bench/suites/judge/calibration"
	"tofu/internal/platform/project"
)

func main() {
	err := report.Generate("calibration count report", func(machine, date string) (string, error) {
		counts, err := calibration.Count(project.RecordedStateDir("log"), time.Now())
		if err != nil {
			return "", err
		}
		verdicts := calibration.Evaluate(counts)
		return calibration.Render(machine, date, counts, verdicts), nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
