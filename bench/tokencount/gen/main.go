package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"tofu/bench/report"
	"tofu/bench/tokencount"
)

const testFunctionCount = 11

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	render := func(machine, date string) (string, error) {
		sessionsDir := filepath.Join("..", "..", "..", ".tofu", "sessions")
		result, err := tokencount.Run(sessionsDir)
		if err != nil {
			return "", err
		}
		return tokencount.Render(tokencount.ReportInput{
			Date:      date,
			Machine:   machine,
			TestCount: testFunctionCount,
			Result:    result,
		}), nil
	}
	genErr := report.Generate("tokencount report", render)
	if genErr == nil {
		return nil
	}
	if !strings.Contains(genErr.Error(), "already on disk") {
		return genErr
	}
	return writeAccountable(render)
}

func writeAccountable(render func(machine, date string) (string, error)) error {
	date := time.Now().Format("2006-01-02")
	machine, err := os.Hostname()
	if err != nil {
		machine = "unknown"
	}
	body, err := render(machine, date)
	if err != nil {
		return err
	}
	path := fmt.Sprintf("../report-%s-accountable.md", date)
	if err := report.Write(path, []byte(body), 0o644, "tokencount report"); err != nil {
		return err
	}
	fmt.Println(path, "written")
	return nil
}
