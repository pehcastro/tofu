package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	genErr := report.Generate("tool call failure taxonomy", render)
	if genErr == nil {
		return nil
	}
	if !strings.Contains(genErr.Error(), "already on disk") {
		return genErr
	}
	return writeCategorized(render)
}

func writeCategorized(render func(machine, date string) (string, error)) error {
	date := time.Now().Format("2006-01-02")
	machine, err := os.Hostname()
	if err != nil {
		machine = "unknown"
	}
	body, err := render(machine, date)
	if err != nil {
		return err
	}
	path := fmt.Sprintf("../report-%s-categorized.md", date)
	if err := report.Write(path, []byte(body), 0o644, "tool call failure taxonomy"); err != nil {
		return err
	}
	fmt.Println(path, "written")
	return nil
}
