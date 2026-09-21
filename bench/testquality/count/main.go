package main

import (
	"fmt"
	"os"

	"tofu/bench/testquality"
)

func must[T any](value T, err error) T {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	return value
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: count <package-dir>")
		os.Exit(2)
	}
	dir := os.Args[1]
	dead := must(testquality.CountDeadTests(dir))
	pattern := must(testquality.PatternFlagsTautologicalTests(dir))
	judgment := must(testquality.JudgmentFlagsTautologicalTests(dir))
	fmt.Printf("tautological=%d mock_boundary=%d no_boundary_coverage=%d dead_total=%d pattern_tautological=%d judgment_tautological=%d\n",
		dead.Tautological, dead.MockBoundary, dead.NoBoundaryCoverage, dead.Total, len(pattern), len(judgment))
}
