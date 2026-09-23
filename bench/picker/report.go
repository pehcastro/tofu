package picker

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"tofu/bench/corpus"
)

const notSeparable = "n/a"

func Render(result Result) string {
	b := &strings.Builder{}
	fmt.Fprintf(b, "corpus: %s, read at %s\n", strings.Join(result.Corpus.Roots, ", "),
		result.ReadAt.Format("2006-01-02 15:04 -07:00"))
	fmt.Fprintf(b, "%d files, %d rows, %d recorded window readings over %d distinct accounts, %d skipped\n",
		result.Corpus.Files, result.Corpus.Rows, len(result.Corpus.Readings),
		result.Corpus.Accounts, len(result.Corpus.Skips))
	for _, line := range skipLines(result.Corpus.Skips) {
		fmt.Fprintf(b, "  skipped: %s\n", line)
	}
	fmt.Fprintf(b, "%d decision points, one per provider and reading time\n", len(result.Snapshots))

	b.WriteString("\nthe cost of a forced move, from " + result.PrefixPath + ", child_first.cache_read\n")
	for _, prefix := range result.FreshPrefix {
		fmt.Fprintf(b, "  %-26s %7d tokens of fresh prefix\n", prefix.Child, prefix.Tokens)
	}

	b.WriteString("\nthe three arms over the recorded readings\n")
	fmt.Fprintf(b, "%-14s %8s %8s", "arm", "moves", "walls")
	for _, prefix := range result.FreshPrefix {
		fmt.Fprintf(b, " %14s", "at "+strconv.Itoa(prefix.Tokens))
	}
	b.WriteString("\n")
	for _, score := range result.Scores {
		fmt.Fprintf(b, "%-14s %8s %8s", score.Arm, cell(score.Moves, result.Separable), cell(score.Walls, result.Separable))
		for _, cost := range score.Costs {
			fmt.Fprintf(b, " %14s", cell(cost.Total, result.Separable))
		}
		b.WriteString("\n")
	}
	b.WriteString("\n" + verdict(result))
	return b.String()
}

func cell(value int, separable bool) string {
	if !separable {
		return notSeparable
	}
	return strconv.Itoa(value)
}

func verdict(result Result) string {
	if result.Separable {
		return "the corpus separates the arms: read the moves column, then the cost of those moves\n"
	}
	return "the corpus cannot separate the three arms. " +
		strconv.Itoa(len(result.Corpus.Readings)) + " recorded window readings over " +
		strconv.Itoa(result.Corpus.Accounts) + " distinct accounts, and an arm only differs from " +
		"another when two accounts are usable at the same decision point.\n" +
		"what it would take: a recorded window reading per account per poll, written where a bench " +
		"can read it. Nothing in the tree writes one today: the poller caches a report in memory " +
		"and no session, ledger or state row carries it.\n" +
		"no number is offered in place of the answer.\n"
}

func skipLines(skips []corpus.SkippedTurn) []string {
	counts := map[string]int{}
	for _, skip := range skips {
		counts[skip.Path+" is missing "+skip.Reason]++
	}
	lines := make([]string, 0, len(counts))
	for line, count := range counts {
		lines = append(lines, line+", "+strconv.Itoa(count)+" rows")
	}
	sort.Strings(lines)
	return lines
}
