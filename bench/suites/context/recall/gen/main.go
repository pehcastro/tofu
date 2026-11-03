package main

import (
	"fmt"
	"os"
	"time"

	"tofu/bench/kit/report"
	"tofu/bench/suites/context/recall"
	"tofu/internal/sys"
)

func main() {
	sessions := sys.RecordedStateDir("sessions")
	if len(os.Args) > 1 {
		sessions = os.Args[1]
	}
	err := report.Generate("recall reach report", func(machine, date string) (string, error) {
		reach, err := recall.WalkCorpusReach(sessions, time.Now())
		if err != nil {
			return "", err
		}
		return recall.Render(machine, date, reach), nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
