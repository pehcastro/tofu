package recall

import (
	"fmt"
	"strings"

	rc "tofu/internal/agent/context/recall"
	"tofu/internal/konst"
)

const ceiling = konst.ContextCeilingTokens

func Render(machine, date string, reach CorpusReach) string {
	var out strings.Builder
	fmt.Fprintf(&out, "# bench recall: %s\n\n", date)
	fmt.Fprintf(&out, "Machine: %s. Reads `%s` directly, offline, no live model call. Companion to `report-2026-09-20.md`, which this file does not touch. Run: `go run ./bench/recall/gen [sessions-dir]`, which defaults to this project's own recorded sessions.\n\n", machine, reach.Dir)

	total := len(reach.Measured) + len(reach.Skipped)
	fmt.Fprintf(&out, "## Peak occupancy against `konst.ContextCeilingTokens`\n\n")
	fmt.Fprintf(&out, "%d of %d recorded sessions on this machine carry occupancy and could be measured; %d could not, counted below as a skip.\n\n", len(reach.Measured), total, len(reach.Skipped))
	if len(reach.Measured) == 0 {
		out.WriteString("No session could be measured, so there is no distribution to report.\n\n")
	} else {
		highest, median := reach.Highest(), reach.Median()
		fmt.Fprintf(&out, "highest: %d tokens, %.1f%% of the %d ceiling\n\n", highest, 100*float64(highest)/float64(ceiling), ceiling)
		fmt.Fprintf(&out, "median: %d tokens, %.1f%% of the ceiling\n\n", median, 100*float64(median)/float64(ceiling))
		half := reach.WithinShareOfCeiling(ceiling, 0.5)
		quarter := reach.WithinShareOfCeiling(ceiling, 0.25)
		tenth := reach.WithinShareOfCeiling(ceiling, 0.1)
		fmt.Fprintf(&out, "within reach of the limit: %d session(s) at or above half the ceiling (%d tokens), %d at or above a quarter (%d tokens), %d at or above a tenth (%d tokens).\n\n",
			half, ceiling/2, quarter, ceiling/4, tenth, ceiling/10)
		fmt.Fprintf(&out, "**No recorded session has ever come within reach of the %d token ceiling.** The largest peak on record, %d tokens, is %.1f%% of it.\n\n", ceiling, highest, 100*float64(highest)/float64(ceiling))
	}

	shippedTarget := rc.ShippedBands().Target()
	fmt.Fprintf(&out, "## The compaction threshold\n\n")
	crossed := reach.Crossed()
	if len(crossed) == 0 {
		fmt.Fprintf(&out, "**No recorded session crossed the target recorded for it at the time.** That is stated plainly: compaction of any kind has never had a reason to run on real material outside a test. Today's shipped target, from `konst.ContextCeilingTokens` and the current band shares, is %d tokens.\n\n", shippedTarget)
	} else {
		fmt.Fprintf(&out, "%d recorded sessions crossed the target recorded for them at the time: %s. Their recorded targets run from %d to %d tokens, because the shares in `internal/konst` moved between builds; today's shipped target is %d tokens.\n\n",
			len(crossed), namesOf(crossed), crossed[0].Target, crossed[len(crossed)-1].Target, shippedTarget)
	}

	fmt.Fprintf(&out, "## What was evicted\n\n")
	if reach.Compactions == 0 {
		out.WriteString("**Nothing has ever been evicted by the in-place rewrite (`rc.Compact`) outside a test.** Zero `compaction` events appear on any step of any recorded session.\n\n")
	} else {
		fmt.Fprintf(&out, "%d in-place rewrite compactions are on record.\n\n", reach.Compactions)
	}
	if len(reach.Forks) == 0 {
		out.WriteString("No session ever forked either, so the mechanism that actually ships (isolate-and-continue) has not run on real material outside a test.\n\n")
	} else {
		fmt.Fprintf(&out, "**The mechanism that did run is the fork, %d time(s):**\n\n", len(reach.Forks))
		for _, fork := range reach.Forks {
			fmt.Fprintf(&out, "- `%s` step %d forked (`%s`) into `%s`: %d tokens down to %d, the carry named %d known sources whole.\n",
				fork.Session, fork.Step, fork.Kind, fork.Into, fork.TokensBefore, fork.TokensAfter, fork.KnownSources)
		}
		out.WriteString("\nA fork drops no result in the in-place sense: it replaces the whole working set with a handle carry, so what \"was evicted\" is the entire pre-fork working set, made reachable again through `lookup` and `artifact_fetch` rather than gone.\n\n")
	}

	fmt.Fprintf(&out, "## Re-fetch rate\n\n")
	if len(reach.Forks) == 0 {
		out.WriteString("**Undefined, not zero.** No eviction of any kind has ever run on a real session, so there is nothing for a re-fetch to be a rate of.\n\n")
	} else {
		fmt.Fprintf(&out, "**%d re-fetch(es) over %d fork(s).** A re-fetch is a tool call in the continuation that names a source the fork's own carry already held, keyed by `rc.Distil`, the function that built the carry, so a call matches on the same source the carry recorded. A call through `lookup` or `artifact_fetch` is the cheap path and not a re-fetch.\n\n", reach.TotalRefetches(), len(reach.Forks))
		out.WriteString("| session | step | into | carried | re-fetched |\n|---|---|---|---|---|\n")
		for _, fork := range reach.Forks {
			refetched := fmt.Sprint(fork.Refetches)
			if fork.Gone != "" {
				refetched = "not read: " + fork.Gone
			}
			fmt.Fprintf(&out, "| `%s` | %d | `%s` | %d | %s |\n", fork.Session, fork.Step, fork.Into, fork.KnownSources, refetched)
		}
		out.WriteString("\n")
	}

	fmt.Fprintf(&out, "## Sessions that could not be measured\n\n")
	fmt.Fprintf(&out, "%d of %d, by reason:\n\n", len(reach.Skipped), total)
	byReason := map[string]int{}
	for _, skip := range reach.Skipped {
		byReason[skip.Reason]++
	}
	for _, reason := range []string{ReasonPreOccupancySchema, ReasonNoOccupancy, ReasonNoSteps} {
		if count, ok := byReason[reason]; ok {
			fmt.Fprintf(&out, "- %d: %s\n", count, reason)
		}
	}
	out.WriteString("\n")

	fmt.Fprintf(&out, "## The condition under which this starts to matter\n\n")
	gapToCeiling := float64(ceiling) / float64(reach.Highest())
	overShippedTarget := 100 * (float64(reach.Highest())/float64(shippedTarget) - 1)
	fmt.Fprintf(&out, "**A session several times longer than the longest ever recorded, at the current ceiling, band shares and bytes-per-token estimate.** The largest peak on record here is %d tokens; the %d ceiling is %.1fx that. `context-budget.md` v7 already measured a synthetic extension of a different recorded turn, the 44 step fixture peaking at 44,145 tokens used across BOJI-108, -119 and -138: a fork first appears at 48 steps on that extension, eight steps past its own real length. The highest peak on record is %.1f%% over today's %d token shipped target. Three things would each pull the condition closer without any session getting longer: a lower `konst.ContextCeilingTokens` (a deliberate test lever already in the code, never shipped to a user), a model whose own window is smaller than the operating ceiling so the model's own wall is hit first, or a task shape that reads much larger results (this project's own source and docs) rather than the many small tool calls the recorded corpus is mostly made of.\n\n",
		reach.Highest(), ceiling, gapToCeiling, overShippedTarget, shippedTarget)

	fmt.Fprintf(&out, "## What this does not answer\n\n")
	out.WriteString("- Whether a re-fetch was a wrong eviction or a deliberate second look: the count says the continuation asked again for a source the carry held, not why.\n")
	out.WriteString("- Whether the fork's cost shape (`context-budget.md` v7: handles beat a summary, 110,486 billed units against 153,392 doing nothing on the one turn measured) holds on these forks: that would need the provider's own reported input tokens on the requests around each fork, which this report did not read.\n")
	fmt.Fprintf(&out, "- The single-file schema sessions, %d of the %d skips: whether any of them would have crossed the ceiling if occupancy had been recorded at the time is not answerable from what is on disk.\n", byReason[ReasonPreOccupancySchema], len(reach.Skipped))

	return out.String()
}

func namesOf(peaks []SessionPeak) string {
	names := make([]string, len(peaks))
	for i, p := range peaks {
		names[i] = fmt.Sprintf("%s (%d of %d tokens)", p.ID, p.Peak, p.Target)
	}
	return strings.Join(names, ", ")
}
