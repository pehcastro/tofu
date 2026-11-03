package toolerrors

import (
	"fmt"
	"strings"
)

func Render(result Result) string {
	b := &strings.Builder{}
	fmt.Fprintf(b, "corpus: %s, read at %s\n", result.SessionsDir, result.ReadAt.Format("2006-01-02 15:04 -07:00"))
	fmt.Fprintf(b, "%d entries, %d read as sessions, %d skipped\n", result.Entries, result.Sessions, len(result.Skips))
	for _, skip := range result.Skips {
		fmt.Fprintf(b, "  skipped: %s: %s\n", skip.Path, skip.Reason)
	}
	fmt.Fprintf(b, "%d tool calls, %d failed (%.1f%%)\n", result.Calls, result.Failures, rate(result.Failures, result.Calls))
	b.WriteString("the only reader is bench/corpus.WalkSessions\n")

	b.WriteString("\n1. per tool, calls and failures\n")
	fmt.Fprintf(b, "%-20s %8s %8s %10s\n", "tool", "calls", "failed", "rate")
	for _, row := range result.Tools {
		fmt.Fprintf(b, "%-20s %8d %8d %9.1f%%\n", row.Tool, row.Calls, row.Failures, rate(row.Failures, row.Calls))
	}

	b.WriteString("\n2. failure share by category\n")
	for _, category := range Categories {
		n := result.ByCategory[category]
		fmt.Fprintf(b, "%-30s %6d %9.1f%%\n", category, n, rate(n, result.Failures))
	}
	fmt.Fprintf(b, "unknown share: %.1f%% of failures\n", rate(result.ByCategory[Unknown], result.Failures))

	b.WriteString("\n3. worst tool by failure rate\n")
	if result.WorstFound {
		fmt.Fprintf(b, "worst tool: %s, %d of %d calls failed, %.1f%%, floor of %d calls to qualify\n",
			result.Worst.Tool, result.Worst.Failures, result.Worst.Calls, rate(result.Worst.Failures, result.Worst.Calls), minCallsForWorst)
		fmt.Fprintf(b, "one real failing call for %s:\n  turn: %s\n  command: %s\n  error: %s\n  category: %s\n",
			result.Worst.Example.Tool, result.Worst.Example.Turn, result.Worst.Example.Command, result.Worst.Example.Error, result.Worst.Example.Category)
	} else {
		b.WriteString("no tool reached the floor of calls, so no worst tool is named\n")
	}
	return b.String()
}

func rate(n, of int) float64 {
	if of == 0 {
		return 0
	}
	return 100 * float64(n) / float64(of)
}
