package main

import (
	"fmt"
	"os"
	"path/filepath"

	"tofu/bench/report"
	"tofu/bench/toolerrors"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	result, err := toolerrors.Run(filepath.Join("..", "..", "..", ".tofu", "sessions"))
	if err != nil {
		return err
	}
	render := func(machine, date string) (string, error) {
		return toolerrors.Markdown(machine, date, result), nil
	}
	return report.Generate("tool call failure taxonomy", render)
}
