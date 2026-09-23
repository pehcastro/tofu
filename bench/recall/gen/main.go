package main

import (
	"fmt"
	"os"
	"path/filepath"

	"tofu/bench/recall"
	"tofu/bench/report"
)

func main() {
	err := report.Generate("recall reach report", func(machine, date string) (string, error) {
		sessionsDir := filepath.Join("..", "..", "..", ".tofu", "sessions")
		reach, err := recall.WalkCorpusReach(sessionsDir)
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
