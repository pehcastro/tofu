package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"tofu/bench/report"
	"tofu/bench/rules"
)

func main() {
	if err := generate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func generate() error {
	logDir := filepath.Join("..", "..", "..", ".tofu", "log")
	libraryDir := filepath.Join("..", "..", "..", "library")
	catalog, err := rules.StructuralCatalog(libraryDir)
	if err != nil {
		return err
	}
	fires, unreadable, err := rules.ReadDir(logDir)
	if err != nil {
		return err
	}
	counts := rules.Count(catalog, fires)
	body := rules.Render(hostname(), time.Now().Format("2006-01-02"), counts, unreadable)
	path := filepath.Join("..", fmt.Sprintf("report-%s.md", time.Now().Format("2006-01-02")))
	if err := report.Write(path, []byte(body), 0o644, "rules fire report"); err != nil {
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
