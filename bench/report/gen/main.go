package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"tofu/bench/report"
)

func main() {
	root := flag.String("bench", "bench", "the bench root to read")
	flag.Parse()
	if err := generate(*root); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func generate(root string) error {
	data, err := report.Build(root)
	if err != nil {
		return err
	}
	machine, err := data.JSON()
	if err != nil {
		return err
	}
	for name, body := range map[string][]byte{
		report.ViewerFile: []byte(report.Page(data)),
		report.DataFile:   machine,
		"INDEX.md":        []byte(data.Markdown()),
	} {
		if err := os.WriteFile(filepath.Join(root, "report", name), body, 0o644); err != nil {
			return err
		}
	}
	fmt.Printf("%s, %s and INDEX.md written under %s. %d of %d Jev decisions have a cheaper method measured beside them, %d reports over %d benches, %d with none, %d withdrawn, %d stale, %d naming no answer.\n",
		report.ViewerFile, report.DataFile, filepath.Join(root, "report"),
		data.ComparedDecisions(), len(data.Judgments),
		data.Counts.Reports, data.Counts.MeasuredPackage, data.Counts.NoReport,
		data.Counts.Withdrawn, data.Counts.Stale, data.Counts.Unparsed)
	return nil
}
