package main

import (
	"fmt"
	"os"

	"tofu/bench/report"
	"tofu/bench/sweep"
)

func main() {
	err := report.Generate("cross-package sweep report", func(machine, date string) (string, error) {
		lines, err := sweep.CountLines("../..")
		if err != nil {
			return "", err
		}
		return sweep.Render(machine, date, lines), nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
