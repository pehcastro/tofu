package prompts

import (
	"fmt"
	"sort"
	"strings"

	"tofu/bench/thrift"
)

const worstSessionsShown = 8

func Render(result Result) string {
	b := &strings.Builder{}
	fmt.Fprintf(b, "corpus: %s, read at %s\n", result.SessionsDir, result.ReadAt.Format("2006-01-02 15:04 -07:00"))
	fmt.Fprintf(b, "%d entries, %d read as sessions, %d skipped, %d distinct task texts\n",
		result.Entries, len(result.Sessions), len(result.Skips), result.DistinctTasks)
	for _, skip := range result.Skips {
		fmt.Fprintf(b, "  skipped: %s: %s\n", skip.Path, skip.Reason)
	}
	fmt.Fprintf(b, "missing field, counted not skipped: %d sessions carry no wall clock, %d carry no task text\n",
		result.NoWallClock, len(result.EmptyTask))
	for _, id := range result.EmptyTask {
		fmt.Fprintf(b, "  no task text: %s\n", id)
	}
	b.WriteString("the only reader is bench/corpus.WalkSessions\n")

	b.WriteString("\n1. what each side of the question looks like per session\n")
	fmt.Fprintf(b, "%-30s %8s %8s %8s %8s %8s %10s\n", "quantity", "sum", "zeros", "p25", "median", "p90", "worst")
	writeSpread(b, "read and search tokens", result.ReadTokens)
	writeSpread(b, "read and search calls", result.ReadCalls)
	writeSpread(b, "wrong directions", result.WrongPerSet)

	fmt.Fprintf(b, "\n2. can either side separate two arms at all, at %d sessions per arm\n", armSessions)
	for _, entry := range result.Separables {
		fmt.Fprintf(b, "%-30s mean %10.2f sd %10.2f smallest visible difference %10.2f, %s\n",
			entry.Name, entry.Mean, entry.SD, entry.Difference, timesTheMean(entry.Difference, entry.Mean))
	}

	fmt.Fprintf(b, "\n3. the two sides crossed, at a sweep of read-volume thresholds\n%10s %6s %14s %8s %8s %9s %8s\n",
		"threshold", "both", "distinct both", "volume", "wrong", "neither", "phi")
	for _, table := range result.Tables {
		fmt.Fprintf(b, "%10d %6d %14d %8d %8d %9d %8s\n",
			table.ThresholdTokens, table.VolumeAndWrong, table.DistinctBoth,
			table.VolumeOnly, table.WrongOnly, table.Neither, number(table.Phi))
	}
	fmt.Fprintf(b, "headline threshold %d tokens: gemini-cli builds a 1,000 line file before it asserts thrift on a task,\n", headlineThresholdTokens)
	b.WriteString("which is about 10,000 tokens of read volume, so a session under it has nothing for a thrift arm to save\n")

	b.WriteString("\n4. the correlation between read volume and wrong directions\n")
	fmt.Fprintf(b, "%-56s %6s %10s %10s\n", "set", "n", "pearson", "spearman")
	for _, correlation := range result.Correlations {
		fmt.Fprintf(b, "%-56s %6d %10s %10s\n", correlation.Set, correlation.N,
			number(correlation.Pearson), number(correlation.Spearman))
	}

	fmt.Fprintf(b, "\n5. leakage: %d sessions name the thing being measured in their own task text\n", len(result.Leaks))
	for _, leak := range result.Leaks {
		fmt.Fprintf(b, "  %s: %q\n", leak.ID, leak.Term)
	}

	fmt.Fprintf(b, "\n6. the %d sessions carrying the most wrong directions\n%-26s %7s %10s %12s %10s %8s\n",
		worstSessionsShown, "session", "calls", "read calls", "read tokens", "retreats", "contra")
	rows := append([]Session(nil), result.Sessions...)
	sort.Slice(rows, func(i, j int) bool { return rows[i].WrongDirections() > rows[j].WrongDirections() })
	for _, row := range rows[:min(worstSessionsShown, len(rows))] {
		fmt.Fprintf(b, "%-26s %7d %10d %12d %10d %8d\n",
			row.ID, row.Calls, row.ReadCalls, row.ReadTokens, row.Retreats, row.Contradictions)
	}
	return b.String()
}

func writeSpread(b *strings.Builder, name string, spread thrift.Spread) {
	fmt.Fprintf(b, "%-30s %8d %8d %8d %8d %8d %10d\n",
		name, spread.Sum, spread.Zeros, spread.P25, spread.Median, spread.P90, spread.Worst)
}

func number(value float64) string {
	if value != value {
		return "undefined"
	}
	return fmt.Sprintf("%.3f", value)
}

func timesTheMean(value, mean float64) string {
	if mean == 0 {
		return "unbounded against a mean of zero"
	}
	return fmt.Sprintf("%.1f times the mean", value/mean)
}
