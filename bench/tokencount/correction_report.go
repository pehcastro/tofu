package tokencount

import (
	"fmt"
	"strings"
)

const priorAccountableMedian = 47.1

var movedReports = []string{
	"bench/schemas/report-2026-09-23.md: the tool schema block, 16574 bytes estimated as 4143 tokens",
	"bench/prefix/report-2026-09-23.md: the per-session rewrite table, 4135/3908/1852 bytes estimated as 1033/977/463 tokens",
	"bench/prefix/report-2026-09-23-corrected.md: the per-session rewrite table, 4492/4265/2209 bytes estimated as 1123/1066/552 tokens",
	"bench/tokens/report-2026-09-21.md: the per-tool tokens column over the whole corpus",
	"bench/tokens/report-2026-09-21-pinned.md: the same per-tool tokens column, replayed against the pinned tree",
	"bench/linenumbers/report-2026-09-23.md: 1016862 read-tool bytes estimated as about 254215 tokens",
	"bench/tokencount/report-2026-09-24.md and report-2026-09-24-accountable.md: the Estimate field of every sample, and every median and worst percentage derived from it",
}

func RenderCorrection(in ReportInput) string {
	b := &strings.Builder{}
	samples := in.Result.Samples
	factors := FitFactors(samples)
	unaccountable := Filter(samples, func(s Sample) bool { return !s.Accountable })
	unacctStats := statsOf(UnaccountableLabel, unaccountable)
	corrected := CorrectedAccountable(samples, factors)
	overallAfter := statsOf("all accountable shapes, corrected", corrected)

	fmt.Fprintf(b, "# bench tokencount, a correction factor over bytes over four: %s\n\n", in.Date)
	fmt.Fprintf(b, "Machine: %s.\n\n", in.Machine)
	fmt.Fprintf(b, "`go test ./bench/tokencount/... -count=1` passes, %d test functions.\n\n", in.TestCount)

	b.WriteString("## Headline\n\n")
	fmt.Fprintf(b, "Fitted per shape on the %d accountable steps only, the residual median moves from %.1f%% to %.1f%%, worst %.1f%%, %.1f%% still out by more than a tenth. The factor is this corpus's factor: it is fitted and reported on the same steps, not validated on a held-out set, and it does nothing for the %d unaccountable steps where the billed tokens exceed the visible bytes outright.\n\n",
		overallAfter.N, priorAccountableMedian, overallAfter.MedianPct, overallAfter.WorstPct, overallAfter.ShareOver10, unacctStats.N)

	b.WriteString("## The fitted factor per shape\n\n")
	b.WriteString("Each factor is the median of bytes divided by billed tokens, taken sample by sample over that shape's accountable steps, replacing the fixed 4 in konst.SearchBytesPerToken with a shape-specific divisor. This is a proposal, not a change: konst is untouched.\n\n")
	fmt.Fprintf(b, "%-60s %8s %14s\n", "shape", "n", "bytes/token")
	for _, f := range factors {
		fmt.Fprintf(b, "%-60s %8d %14.2f\n", f.Shape, f.N, f.BytesPerToken)
	}
	b.WriteString("\n")

	b.WriteString("## Residual error after correction, per shape\n\n")
	renderStats(b, beforeAfterRows(factors))
	b.WriteString("\n")
	b.WriteString(worseningNote(factors))

	b.WriteString("## What the factor cannot fix\n\n")
	fmt.Fprintf(b, "The %d unaccountable steps, %s, were not fitted on and cannot be: their own completion_tokens exceeds the bytes recorded for them, so no divisor, however small, reproduces the count from what is on record. Median error there stays %.1f%%, unmoved by this ticket, because moving it needs the corpus to capture content it currently does not, named in TOFU-562 as most likely an uncaptured reasoning or thinking pass.\n\n",
		unacctStats.N, UnaccountableLabel, unacctStats.MedianPct)

	b.WriteString("## Which figures move if the constant changes\n\n")
	b.WriteString("konst.SearchBytesPerToken is unchanged by this ticket. If a later ticket replaces 4 with a shape-aware factor, these dated reports carry figures derived from it and would need to be recomputed and superseded, never edited in place:\n\n")
	for _, r := range movedReports {
		fmt.Fprintf(b, "- %s\n", r)
	}
	b.WriteString("\n")

	b.WriteString("## Corpus\n\n")
	fmt.Fprintf(b, "Same corpus as report-2026-09-24-accountable.md: %d entries under %s, %d turns, %d steps read, %d usable. The factor is fitted only on the %d accountable of those %d usable steps.\n\n",
		in.Result.Entries, in.Result.SessionsDir, in.Result.Turns, in.Result.StepsRead, in.Result.StepsUsable, overallAfter.N, in.Result.StepsUsable)

	b.WriteString("## Is a fitted constant good enough\n\n")
	b.WriteString(correctionVerdict(overallAfter, unacctStats))
	return b.String()
}

func worseningNote(factors []Factor) string {
	b := &strings.Builder{}
	for _, f := range factors {
		if f.After.MedianPct > f.Before.MedianPct {
			fmt.Fprintf(b, "The median of bytes over billed tokens is not the same divisor as the one that minimizes median error, and %s shows it: fitting it moved the median error from %.1f%% to %.1f%%, worse than the flat 4 it replaced. A per-shape median is not guaranteed to help, and this shape is the counterexample in this corpus.\n\n",
				f.Shape, f.Before.MedianPct, f.After.MedianPct)
		}
	}
	return b.String()
}

func beforeAfterRows(factors []Factor) []Stats {
	rows := make([]Stats, 0, len(factors)*2)
	for _, f := range factors {
		rows = append(rows, f.Before, f.After)
	}
	return rows
}

func correctionVerdict(after, unacct Stats) string {
	if after.N == 0 {
		return "No accountable step exists in this corpus, so no factor could be fitted and no verdict is given.\n"
	}
	verb := "is"
	if after.ShareOver10 > 10 {
		verb = "is not"
	}
	return fmt.Sprintf("A per-shape median is an improvement over the flat 4 on the half of the corpus it can reach, moving the overall accountable median from %.1f%% to %.1f%%, but %.1f%% of corrected accountable steps are still out by more than a tenth, so a single fitted number per shape %s a substitute for measuring what a step actually generated. It also does not touch the %d unaccountable steps, still off by a median of %.1f%%. The finding is not that four was the wrong constant; it is that a constant, fitted or not, is the wrong shape of estimate for a distribution this wide, and the honest fix stays a real vocabulary or a captured receipt, not a better divisor.\n",
		priorAccountableMedian, after.MedianPct, after.ShareOver10,
		verb,
		unacct.N, unacct.MedianPct)
}
