package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"tofu/bench/promote"
	"tofu/bench/report"
)

func main() {
	err := report.Generate("promotion corpus count", func(machine, date string) (string, error) {
		stateDir := filepath.Join("..", "..", "..", ".tofu")
		rows, err := promote.Read(stateDir)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		return promote.Render(machine, date, promote.Path(".tofu"), rows), nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
