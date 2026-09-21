package main

import (
	"fmt"
	"os"
	"strconv"

	"tofu/bench/testquality/flake"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: flakerun <runs> <package>...")
		os.Exit(2)
	}
	runs, err := strconv.Atoi(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	disagreements := 0
	for _, pkg := range os.Args[2:] {
		report, err := flake.Measure(pkg, runs)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", pkg, err)
			os.Exit(1)
		}
		disagreeing := report.Disagreeing()
		skipped := report.AlwaysSkipped()
		disagreements += len(disagreeing)
		fmt.Printf("%s runs=%d tests=%d disagreed=%d always_skipped=%d\n",
			pkg, report.Runs, len(report.Tests), len(disagreeing), len(skipped))
		for _, test := range disagreeing {
			fmt.Printf("  disagreed %s %v\n", test.Test, test.Outcomes)
		}
		for _, test := range skipped {
			fmt.Printf("  skipped %s\n", test.Test)
		}
	}
	if disagreements > 0 {
		os.Exit(1)
	}
}
