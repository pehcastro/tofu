package main

import (
	"fmt"
	"os"
	"path/filepath"

	"tofu/bench/kit/report"
	"tofu/bench/suites/tools/sweep"
	"tofu/internal/sys"
)

func main() {
	err := report.Generate("cross-package sweep report", func(machine, date string) (string, error) {
		lines, err := sweep.CountLines(filepath.Join(sys.SourceRoot(), "bench"))
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
