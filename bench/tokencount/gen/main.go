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

const testFunctionCount = 16

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	result, err := tokencount.Run(filepath.Join("..", "..", "..", ".tofu", "sessions"))
	if err != nil {
		return err
	}
	render := func(machine, date string) (string, error) {
		return tokencount.Render(tokencount.ReportInput{Date: date, Machine: machine, TestCount: testFunctionCount, Result: result}), nil
	}
	renderCorrection := func(machine, date string) (string, error) {
		return tokencount.RenderCorrection(tokencount.ReportInput{Date: date, Machine: machine, TestCount: testFunctionCount, Result: result}), nil
	}
	variants := []struct {
		suffix string
		render func(machine, date string) (string, error)
	}{
		{"", render},
		{"-accountable", render},
		{"-corrected", renderCorrection},
	}
	var lastErr error
	for _, v := range variants {
		lastErr = writeVariant(v.suffix, v.render)
		if lastErr == nil || !strings.Contains(lastErr.Error(), "already on disk") {
			return lastErr
		}
	}
	return lastErr
}

func writeVariant(suffix string, render func(machine, date string) (string, error)) error {
	date := time.Now().Format("2006-01-02")
	machine, err := os.Hostname()
	if err != nil {
		machine = "unknown"
	}
	body, err := render(machine, date)
	if err != nil {
		return err
	}
	path := fmt.Sprintf("../report-%s%s.md", date, suffix)
	if err := report.Write(path, []byte(body), 0o644, "tokencount report"); err != nil {
		return err
	}
	fmt.Println(path, "written")
	return nil
}
