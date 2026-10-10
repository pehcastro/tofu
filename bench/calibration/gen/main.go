package main

import (
	"fmt"
	"os"
	"time"

	"tofu/bench/calibration"
	"tofu/bench/report"
	"tofu/internal/sys"
)

func main() {
	err := report.Generate("calibration count report", func(machine, date string) (string, error) {
		counts, err := calibration.Count(sys.RecordedStateDir("log"), time.Now())
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
