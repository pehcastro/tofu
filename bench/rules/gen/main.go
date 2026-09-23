package main

import (
	"fmt"
	"os"
	"path/filepath"

	"tofu/bench/report"
	"tofu/bench/rules"
)

func main() {
	err := report.Generate("rules fire report", func(machine, date string) (string, error) {
		logDir := filepath.Join("..", "..", "..", ".tofu", "log")
		libraryDir := filepath.Join("..", "..", "..", "library")
		catalog, err := rules.StructuralCatalog(libraryDir)
		if err != nil {
			return "", err
		}
		fires, unreadable, err := rules.ReadDir(logDir)
		if err != nil {
			return "", err
		}
		counts := rules.Count(catalog, fires)
		return rules.Render(machine, date, counts, unreadable), nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
