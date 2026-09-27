package main

import (
	"fmt"
	"os"
	"path/filepath"

	"tofu/bench/report"
	"tofu/bench/rules/fire"
	"tofu/internal/sys"
)

func main() {
	err := report.Generate("rules fire report", func(machine, date string) (string, error) {
		logDir := sys.RecordedStateDir("log")
		libraryDir := filepath.Join("..", "..", "..", "library")
		catalog, err := fire.StructuralCatalog(libraryDir)
		if err != nil {
			return "", err
		}
		fires, unreadable, err := fire.ReadDir(logDir)
		if err != nil {
			return "", err
		}
		counts := fire.Count(catalog, fires)
		return fire.Render(machine, date, counts, unreadable), nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
