package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"tofu/bench/calibration"
	"tofu/bench/report"
)

func main() {
	if err := generate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func generate() error {
	dir := filepath.Join("..", "..", "..", ".tofu", "log")
	counts, err := calibration.Count(dir)
	if err != nil {
		return err
	}
	verdicts := calibration.Evaluate(counts)
	body := calibration.Render(hostname(), time.Now().Format("2006-01-02"), counts, verdicts)
	path := filepath.Join("..", fmt.Sprintf("report-%s.md", time.Now().Format("2006-01-02")))
	if err := report.Write(path, []byte(body), 0o644, "calibration count report"); err != nil {
		return err
	}
	fmt.Println(path, "written")
	return nil
}

func hostname() string {
	name, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return name
}
