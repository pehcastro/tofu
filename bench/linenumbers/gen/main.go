package main

import (
	"fmt"
	"os"
	"path/filepath"

	"tofu/bench/linenumbers"
	"tofu/bench/report"
)

func main() {
	err := report.Generate("read tool line-number bytes", func(machine, date string) (string, error) {
		dir := filepath.Join("..", "..", "..", ".tofu", "sessions")
		result, err := linenumbers.Run(dir)
		if err != nil {
			return "", err
		}
		return linenumbers.Render(machine, date, result), nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
