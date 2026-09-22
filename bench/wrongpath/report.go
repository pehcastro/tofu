package wrongpath

import (
	"fmt"
	"sort"
	"strings"
)

const worstSessionsShown = 5

func Render(result Result) string {
	b := &strings.Builder{}
	fmt.Fprintf(b, "corpus: %s, read at %s\n", result.SessionsDir, result.ReadAt.Format("2006-01-02 15:04 -07:00"))
	fmt.Fprintf(b, "%d entries, %d read as sessions, %d skipped, %d carrying no wall clock\n",
		result.Entries, result.Sessions, len(result.Skips), result.NoWallClock)
	fmt.Fprintf(b, "%d tool calls, of which %d write or edit a file, and %d sessions carry no shape at all\n",
		result.Calls, result.MutationCalls, result.CleanSessions)
	for _, skip := range result.Skips {
		fmt.Fprintf(b, "  skipped: %s: %s\n", skip.Path, skip.Reason)
	}
	b.WriteString("the only reader is bench/corpus.WalkSessions\n")

	fmt.Fprintf(b, "\n1. the three shapes over every session\n%-26s %8s %10s %8s %8s %8s %8s %8s %12s\n",
		"shape", "total", "none", "median", "p90", "worst", "mean", "sd", "per 100 calls")
	for _, shape := range result.Shapes {
		fmt.Fprintf(b, "%-26s %8d %10d %8.1f %8.1f %8d %8.2f %8.2f %12.2f\n",
			shape.Name, shape.PerSession.Sum, shape.PerSession.Zeros, shape.PerSession.Median,
			shape.PerSession.P90, shape.PerSession.Worst, shape.PerSession.Mean, shape.PerSession.SD, shape.RatePerCalls)
	}

	b.WriteString("\n2. what each shape counts and what it counts wrongly\n")
	for _, shape := range result.Shapes {
		fmt.Fprintf(b, "%s\n  rule: %s\n  false positive: %s\n", shape.Name, shape.Rule, shape.FalsePositive)
	}

	b.WriteString("\n3. could this separate two arms\n")
	b.WriteString("the harness rule in bench/harness/report.go calls a difference smaller than the widest spread no difference\n")
	for _, shape := range result.Shapes {
		fmt.Fprintf(b, "\n%s: mean %.2f per session, widest spread %d\n", shape.Name, shape.PerSession.Mean, shape.PerSession.Range)
		fmt.Fprintf(b, "  under the harness rule two arms must differ by %d per session, which is %s the mean\n",
			shape.PerSession.Range, timesTheMean(float64(shape.PerSession.Range), shape.PerSession.Mean))
		for _, detectable := range shape.Detectable {
			fmt.Fprintf(b, "  at %d sessions per arm a difference of %.2f per session is visible, %s the mean%s\n",
				detectable.ArmSessions, detectable.Difference, timesTheMean(detectable.Difference, shape.PerSession.Mean),
				verdict(detectable.FractionOfMean))
		}
	}

	fmt.Fprintf(b, "\n4. the %d worst sessions by every shape added together\n%-26s %7s %8s %6s %8s %6s %8s\n",
		worstSessionsShown, "session", "calls", "reverts", "undos", "re-reads", "ident", "contra")
	rows := append([]SessionRow(nil), result.Rows...)
	sort.Slice(rows, func(i, j int) bool { return total(rows[i].Counts) > total(rows[j].Counts) })
	for _, row := range rows[:min(worstSessionsShown, len(rows))] {
		fmt.Fprintf(b, "%-26s %7d %8d %6d %8d %6d %8d\n", row.ID, row.Calls,
			row.Counts.Reverts, row.Counts.ExactUndos, row.Counts.ReReads, row.Counts.IdenticalReReads, row.Counts.Contradictions)
	}
	return b.String()
}

func total(counts Counts) int {
	return counts.Reverts + counts.ExactUndos + counts.ReReads + counts.IdenticalReReads + counts.Contradictions
}

func timesTheMean(value, mean float64) string {
	if mean == 0 {
		return "unbounded against a mean of zero"
	}
	return fmt.Sprintf("%.1f times", value/mean)
}

func verdict(fraction float64) string {
	switch {
	case fraction <= 0.5:
		return ", so an arm that halves the shape would show"
	case fraction <= 1:
		return ", so only an arm that wipes the shape out would show"
	default:
		return ", so not even wiping the shape out would show"
	}
}
