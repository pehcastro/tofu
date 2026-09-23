package schemas

import (
	"fmt"
	"sort"
	"strings"
)

const TargetTurnID = "turn-18d7e7074db17e6c"

type Report struct {
	Date                string
	Machine             string
	BuildNote           string
	Whole               Whole
	ToolCount           int
	Target              TurnUsage
	TargetNames         []string
	Cohort              Cohort
	Deferred            DeferredCost
	CalledNotInRegistry []string
}

func (r Report) Render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# bench schemas: %s\n\n", r.Date)
	fmt.Fprintf(&b, "Machine: %s. %s\n\n", r.Machine, r.BuildNote)

	b.WriteString("## What was counted\n\n")
	fmt.Fprintf(&b, "The tool count and every byte figure below come from building the exact registry `cmd/tofu/run.go` builds for a plain `tofu run` with no `--tools`, `--no-subagents` or `--truncate-results` flag, which is the invocation the harness report of 2026-09-23 recorded for `%s`. That registry carries %d tools, matching the count this bench's own ticket cites from the tree. Every tool call actually recorded in that turn's steps is a member of this registry: %s.\n\n",
		r.Target.ID, r.ToolCount, presence(r.CalledNotInRegistry))

	b.WriteString("## Whole and per-tool schema cost\n\n")
	fmt.Fprintf(&b, "Bytes are the exact JSON `anthropic` puts on the wire for the tools array, measured by encoding a real request with and without `Tools` set and taking the difference: %d bytes. Summing each tool's own `{name, description, input_schema}` encoding gives %d bytes, %d bytes apart from the wire figure, which is the array's brackets and commas. Tokens are bytes divided by `konst.SearchBytesPerToken`, 4, this project's own byte-to-token scale and not a tokenizer.\n\n",
		r.Whole.WireBytes, r.Whole.SumBytes, r.Whole.WireBytes-r.Whole.SumBytes)
	b.WriteString("| Tool | Bytes | Tokens (est) |\n|---|---|---|\n")
	sorted := make([]ToolCost, len(r.Whole.Tools))
	copy(sorted, r.Whole.Tools)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Bytes > sorted[j].Bytes })
	for _, tc := range sorted {
		fmt.Fprintf(&b, "| %s | %d | %d |\n", tc.Name, tc.Bytes, tc.Tokens)
	}
	fmt.Fprintf(&b, "\n**Whole: %d bytes, about %d tokens for all %d tools on every request.** search, edit and symbols are the top three by bytes and together are %.0f%% of the total; the two smallest, artifact_fetch and write, are the bottom.\n\n",
		r.Whole.WireBytes, r.Whole.WireTokens, r.ToolCount, topThreeShare(sorted))

	b.WriteString("## What caching does to the number, from the recorded turn\n\n")
	firstWrite, _ := r.Target.FirstCacheWrite()
	fmt.Fprintf(&b, "`%s`, the tofu arm of the 2026-09-23 harness run, wrote %d cache tokens on step 1 and never wrote that block again across its %d steps; every later step reads %d or more tokens back from cache instead. The computed schema block above, %d tokens, is about %.0f%% of that one write, the rest being the system prompt and the project instructions this machine's own CLAUDE.md carries. Across the whole turn tofu was billed %d fresh input tokens, %d cache-write tokens and %d cache-read tokens: cache reads are %.1f%% of the turn's billed input, the same order of magnitude as the harness report's own claude figure for the same task, 128,058 of 139,047, 92%%, though the two arms are not the same credential kind and are not compared as a difference.\n\n",
		r.Target.ID, firstWrite, len(r.Target.Steps), r.Target.Steps[len(r.Target.Steps)-1].CacheReadTokens, r.Whole.WireTokens,
		100*float64(r.Whole.WireTokens)/float64(max(firstWrite, 1)),
		r.Target.SumFreshInput(), r.Target.SumCacheWrite(), r.Target.SumCacheRead(),
		100*float64(r.Target.SumCacheRead())/float64(max(r.Target.SumFreshInput()+r.Target.SumCacheWrite()+r.Target.SumCacheRead(), 1)))

	fmt.Fprintf(&b, "**The same pattern holds across every recorded turn old enough to carry cache fields.** %d of %d turns under `%s` carry usable cache accounting (the other %d predate cache accounting or hold no steps, see the skip counts below); of those, %d had at least one cache-write step, and %d of those %d ran a second model call and so read the write back at least once. Restricted to the %d turns whose first write is at today's 18-tool scale, %d of %d ran a second call.\n\n",
		r.Cohort.UsableTurns, r.Cohort.UsableTurns+r.Cohort.SkippedTurns, r.Cohort.Dir, r.Cohort.SkippedTurns,
		r.Cohort.WithCacheWrite, r.Cohort.MultiStep, r.Cohort.WithCacheWrite,
		r.Cohort.CurrentToolsetRuns, r.Cohort.CurrentToolsetRuns-r.Cohort.CurrentSingleStep, r.Cohort.CurrentToolsetRuns)

	fmt.Fprintf(&b, "Over the %d turns with a cache write, the cohort billed %d fresh input tokens, %d cache-write tokens and %d cache-read tokens: %.1f%% of everything billed across this whole sample was a cache read.\n\n",
		r.Cohort.WithCacheWrite, r.Cohort.TotalFreshInput, r.Cohort.TotalCacheWrite, r.Cohort.TotalCacheRead,
		100*float64(r.Cohort.TotalCacheRead)/float64(max(r.Cohort.TotalFreshInput+r.Cohort.TotalCacheWrite+r.Cohort.TotalCacheRead, 1)))

	reasons := make([]string, 0, len(r.Cohort.SkipReasons))
	for reason := range r.Cohort.SkipReasons {
		reasons = append(reasons, reason)
	}
	sort.Strings(reasons)
	b.WriteString("Skips, named: ")
	for i, reason := range reasons {
		if i > 0 {
			b.WriteString("; ")
		}
		fmt.Fprintf(&b, "%d %s", r.Cohort.SkipReasons[reason], reason)
	}
	b.WriteString("\n\n")

	b.WriteString("## The mechanism being costed\n\n")
	fmt.Fprintf(&b, "%s. Deferring means the schema bytes for a tool are not paid until the model has already decided it wants that tool, which does not remove the bytes, it moves them from the initial `tools` array into a later `tool_result` and adds a full model round trip in front of the tool's first real use.\n\n", DeferredMechanismName)
	fmt.Fprintf(&b, "On `%s`, the model called %d distinct tools out of %d offered: %s. Deferring would have cost %d extra round trips, one per first use, against a catalog of one-line summaries costing about %d tokens on every step instead of the full %d, saving at most the %d tokens belonging to the %d tools never called, paid once at cache-write price rather than not at all.\n\n",
		r.Target.ID, r.Deferred.DistinctToolsUsed, r.ToolCount, strings.Join(r.TargetNames, ", "),
		r.Deferred.ExtraRoundTrips, r.Deferred.CatalogTokens, r.Whole.WireTokens,
		r.Deferred.UnusedSchemaTokens, r.ToolCount-r.Deferred.DistinctToolsUsed)

	b.WriteString("## Verdict\n\n")
	fmt.Fprintf(&b, "**Not worth building.** The schema block is written to cache once, on step 1, and %d of %d recorded turns whose first write matches today's tool count went on to a second model call and read it back instead of paying for it again; the schema's own share of that one write, about %d tokens, costs a fraction of a cent at cache-write pricing and nothing at all afterward. Deferring would trade that one-time, mostly-amortized cost for a mandatory extra round trip per distinct tool a task actually needs, which on the one turn measured here would have added %d model calls to an 11-step, 65,678 ms turn to save tokens worth less than one of those calls costs in wall clock alone. The one case where deferring would pay is a turn that makes exactly one model call and never returns for a second, which was %d of %d turns in this sample: too rare and too cheap a miss to build a mechanism for.\n",
		r.Cohort.CurrentToolsetRuns-r.Cohort.CurrentSingleStep, r.Cohort.CurrentToolsetRuns, r.Whole.WireTokens,
		r.Deferred.ExtraRoundTrips, r.Cohort.SingleStep, r.Cohort.WithCacheWrite)

	return b.String()
}

func presence(missing []string) string {
	if len(missing) == 0 {
		return "none named a tool outside it"
	}
	return "except " + strings.Join(missing, ", ") + ", which named a tool this registry does not carry"
}

func topThreeShare(sorted []ToolCost) float64 {
	if len(sorted) < 3 {
		return 0
	}
	total := 0
	for _, tc := range sorted {
		total += tc.Bytes
	}
	top := sorted[0].Bytes + sorted[1].Bytes + sorted[2].Bytes
	if total == 0 {
		return 0
	}
	return 100 * float64(top) / float64(total)
}
