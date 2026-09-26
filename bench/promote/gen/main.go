package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"tofu/bench/promote"
	"tofu/bench/report"
	"tofu/internal/sys"
)

func main() {
	err := report.Generate("promotion corpus count", func(machine, date string) (string, error) {
		stateDir := sys.RecordedStateDir()
		rows, err := promote.Read(stateDir)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		return promote.Render(machine, date, promote.Path(stateDir), rows), nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
