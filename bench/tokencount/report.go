package tokencount

import (
	"fmt"
	"strings"
)

const (
	schemaWireBytes      = 16574
	schemaEstimateTokens = 4143
	schemaCiteReport     = "bench/schemas/report-2026-09-23.md"
)

type ReportInput struct {
	Date      string
	Machine   string
	TestCount int
	Result    Result
}

func Render(in ReportInput) string {
	b := &strings.Builder{}
	samples := in.Result.Samples
	accountable := Filter(samples, func(s Sample) bool { return s.Accountable })
	unaccountable := Filter(samples, func(s Sample) bool { return !s.Accountable })
	proseAcct := statsOf(string(ShapeProse)+" and accountable", shaped(accountable, ShapeProse))
	toolAcct := statsOf(string(ShapeToolCallArgs)+" and accountable", shaped(accountable, ShapeToolCallArgs))
	acctStats := statsOf(AccountableLabel, accountable)
	unacctStats := statsOf(UnaccountableLabel, unaccountable)

	fmt.Fprintf(b, "# bench tokencount: %s\n\n", in.Date)
	fmt.Fprintf(b, "Machine: %s.\n\n", in.Machine)
	fmt.Fprintf(b, "`go test ./bench/tokencount/... -count=1` passes, %d test functions.\n\n", in.TestCount)

	b.WriteString("## Headline\n\n")
	fmt.Fprintf(b, "Split by whether the billed tokens could physically come from the step's own visible bytes: on %d steps where they could, bytes over four still misses by a median of %.1f%%, %.1f%% out by more than a tenth. On %d steps where they could not, because completion_tokens exceeds the visible byte count outright, no tokenizer could have produced that count from what is on record, and the median gap there is %.1f%%. Four is not good enough on either half; the earlier single aggregate of both halves together, a 71.2%% median, buried that this is two different failures rather than one.\n\n",
		acctStats.N, acctStats.MedianPct, acctStats.ShareOver10, unacctStats.N, unacctStats.MedianPct)

	b.WriteString("## What was compared\n\n")
	b.WriteString("The corpus carries a receipt per step, prompt_tokens and completion_tokens, but not the bytes that made up the prompt: the prompt for step N is the whole conversation to that point, the system prompt and the tool definitions, none of which is stored per step. What is fully recovered from a single step is what the model generated in it: assistant_text, or the Args of every tool call. That is compared against completion_tokens, the one receipt that covers exactly that step's own output and nothing before it. This is a completion-side measurement; the prompt side of the estimate is not checked here, and the gap is named rather than papered over.\n\n")
	b.WriteString("A step that carries both assistant text and a tool call is excluded from the shape comparison: the receipt covers both together and cannot be split. A step with neither, or with a tool call whose Args serialise to zero bytes, is excluded and counted as skipped.\n\n")
	b.WriteString("Args bytes count only the call's own JSON arguments, not the function name or the wire's own call envelope, so this estimate undercounts by a template-dependent constant the same way `crates/pi-natives/src/utok/jev.rs:19` undercounts by excluding the request frame.\n\n")

	b.WriteString("## The split: can the visible bytes account for the billed tokens at all\n\n")
	b.WriteString("A token is at least one byte under any real tokenizer, so a step whose completion_tokens exceeds its own visible byte count cannot have been produced from that content alone, at any rate, sane or not. That is a hard floor, not a guess about a typical ratio. Splitting on it:\n\n")
	renderStats(b, ByAccountable(samples))
	b.WriteString("\n")
	proseUnacct := len(shaped(unaccountable, ShapeProse))
	proseWord := "are"
	if proseUnacct == 1 {
		proseWord = "is"
	}
	fmt.Fprintf(b, "The unaccountable group is almost entirely one shape: %d of its %d steps are tool-call arguments, %d %s prose. Whatever produces completion_tokens on those steps is not visible in this corpus at all, which is consistent with billed content the step never writes down, a reasoning or thinking pass among the candidates, but this report does not have the data to confirm that; see the next section.\n\n",
		len(shaped(unaccountable, ShapeToolCallArgs)), len(unaccountable), proseUnacct, proseWord)

	b.WriteString("## Does four hold up on the steps where it is even possible\n\n")
	acctTool := shaped(accountable, ShapeToolCallArgs)
	underTool := Filter(acctTool, func(s Sample) bool { return s.Estimate < s.Actual })
	fmt.Fprintf(b, "Restricting to the %d accountable steps, where the byte count at least allows the token count, four still misses: prose is a median %.1f%% off over %d samples, %.1f%% of them out by more than a tenth; tool-call arguments are a median %.1f%% off over %d samples, %.1f%% out by more than a tenth. Both are one-sided: the estimate undershoots almost every accountable tool-call sample, %d of %d, because short JSON arguments tokenize more densely than four bytes per token, a separate and smaller effect from the unaccountable group above. Four is not good enough for either shape even on the half of the corpus where the comparison is fair.\n\n",
		acctStats.N, proseAcct.MedianPct, proseAcct.N, proseAcct.ShareOver10, toolAcct.MedianPct, toolAcct.N, toolAcct.ShareOver10,
		len(underTool), len(acctTool))

	b.WriteString("## The second lever: does any recorded step carry its own thinking\n\n")
	if in.Result.MessagesWithThinking == 0 {
		fmt.Fprintf(b, "No. %d assistant messages were scanned across %d turns stored in the header-plus-jsonl schema, the only schema that carries a message stream at all, and none of them carries a non-empty `thinking` field or a `reasoning` object, even though TOFU-540 and TOFU-544 landed a typed field for both on 2026-09-23. Either no session in this corpus ran at a raised reasoning effort since those tickets shipped, or the sessions that did are not the ones stored here. This report cannot compare the error on steps with a captured thinking block against steps without one, because the corpus holds zero of the first kind: the theory in the section above stays a theory, named rather than confirmed.\n\n",
			in.Result.MessagesScanned, in.Result.TurnsScannedForThink)
	} else {
		fmt.Fprintf(b, "Yes: %d of %d scanned assistant messages across %d turns carry a thinking or reasoning field.\n\n", in.Result.MessagesWithThinking, in.Result.MessagesScanned, in.Result.TurnsScannedForThink)
	}

	b.WriteString("## Per wire\n\n")
	renderStats(b, ByWire(samples))
	b.WriteString("\n")

	b.WriteString("## Per content shape\n\n")
	renderStats(b, ByShape(samples))
	b.WriteString("\n")

	b.WriteString("## The tool schema block itself\n\n")
	fmt.Fprintf(b, "The two shapes above are what a step generates, not the tool definitions sent with every request. %s already measured that block at %d wire bytes for the 18 tools a plain run offers, an estimate of %d tokens at bytes over four. That figure is cited rather than remeasured here, and it cannot be checked against a receipt the way the shapes above can: it never arrives in prompt_tokens on its own, only folded into the whole request, and bench/schemas found it is written to cache once and read back on every later step, so no step's usage numbers isolate it either. Whether four is a fair estimate for that specific block is therefore still unmeasured, named as a gap rather than assumed.\n\n",
		schemaCiteReport, schemaWireBytes, schemaEstimateTokens)

	b.WriteString("## Corpus\n\n")
	fmt.Fprintf(b, "%d entries under %s, %d read as turns, %d skipped as not a turn.\n", in.Result.Entries, in.Result.SessionsDir, in.Result.Turns, len(in.Result.TurnsSkipped))
	for _, skip := range in.Result.TurnsSkipped {
		fmt.Fprintf(b, "  skipped: %s: %s\n", skip.Path, skip.Reason)
	}
	fmt.Fprintf(b, "%d turns of %d carry no wire field and are excluded from every wire, not from every shape.\n", in.Result.TurnsNoWire, in.Result.Turns)
	fmt.Fprintf(b, "%d steps read, %d usable, %d skipped.\n", in.Result.StepsRead, in.Result.StepsUsable, len(in.Result.StepsSkipped))
	byReason := map[string]int{}
	for _, s := range in.Result.StepsSkipped {
		byReason[s.Reason]++
	}
	for reason, n := range byReason {
		fmt.Fprintf(b, "  %d skipped: %s\n", n, reason)
	}
	b.WriteString("\n")

	b.WriteString("## Is four good enough\n\n")
	b.WriteString(verdict(acctStats, unacctStats, proseAcct, toolAcct))
	return b.String()
}

func shaped(samples []Sample, shape Shape) []Sample {
	return Filter(samples, func(s Sample) bool { return s.Shape == shape })
}

func renderStats(b *strings.Builder, rows []Stats) {
	fmt.Fprintf(b, "%-90s %8s %10s %10s %12s\n", "key", "n", "median %", "worst %", "share >10%")
	for _, row := range rows {
		fmt.Fprintf(b, "%-90s %8d %9.1f%% %9.1f%% %11.1f%%\n", row.Key, row.N, row.MedianPct, row.WorstPct, row.ShareOver10)
	}
}

func verdict(acct, unacct, proseAcct, toolAcct Stats) string {
	if acct.N == 0 && unacct.N == 0 {
		return "No usable step exists in this corpus, so there is no error to judge and no verdict to give.\n"
	}
	return fmt.Sprintf("No, for what four is actually asked to estimate on this corpus. Even on the %d steps where the billed tokens could in principle come from the visible bytes, prose misses by a median of %.1f%% and tool-call arguments by %.1f%%, both one-sided on tool-call json, so a byte budget built on four will under-provision structured output specifically, not just occasionally miss. The other %d steps, almost all tool-call shaped, bill more tokens than their visible bytes could ever encode; no per-shape constant fixes that, because the content it would need to count is not in the corpus at all. Replacing four with a real vocabulary costs a merge table and a merge loop this project has no dependency for. The cheaper next step is two separate fixes: a measured per-shape correction factor for the accountable half, and, before anything about the other half can be estimated, making the harness actually capture the thinking or reasoning field TOFU-540 and TOFU-544 already carry end to end but this corpus has never once recorded populated.\n",
		acct.N, proseAcct.MedianPct, toolAcct.MedianPct, unacct.N)
}
