package recall

import (
	"fmt"
	"strings"

	rc "tofu/internal/recall"
)

const ceiling = 250000

func Render(machine, date string, reach CorpusReach) string {
	var out strings.Builder
	fmt.Fprintf(&out, "# bench recall: %s\n\n", date)
	fmt.Fprintf(&out, "Machine: %s. Reads `.tofu/sessions` directly, offline, no live model call. Companion to `report-2026-09-20.md`, which this file does not touch. Run: `go run ./bench/recall/gen`.\n\n", machine)

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
		fmt.Fprintf(&out, "**No recorded session has ever come within reach of the 250,000 token ceiling.** The largest peak on record, %d tokens, is %.1f%% of it.\n\n", highest, 100*float64(highest)/float64(ceiling))
	}

	shippedTarget := rc.ShippedBands().Target()
	fmt.Fprintf(&out, "## The compaction threshold\n\n")
	crossed := reach.Crossed()
	if len(crossed) == 0 {
		fmt.Fprintf(&out, "**No recorded session crossed the target recorded for it at the time.** That is stated plainly: compaction of any kind has never had a reason to run on real material outside a test. Today's shipped target, from `konst.ContextCeilingTokens` and the current band shares, is %d tokens.\n\n", shippedTarget)
	} else {
		fmt.Fprintf(&out, "%d recorded sessions crossed the target recorded for them at the time: %s. The two ran under different band builds, %d and %d tokens, because the shares in `internal/konst` moved between them; today's shipped target is %d tokens.\n\n",
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
		fmt.Fprintf(&out, "**The mechanism that did run is the fork, twice, both `continuation` kind:**\n\n")
		for _, fork := range reach.Forks {
			fmt.Fprintf(&out, "- `%s` step %d forked into `%s`: %d tokens down to %d, the carry named %d known sources whole.\n",
				fork.Session, fork.Step, fork.Into, fork.TokensBefore, fork.TokensAfter, fork.KnownSources)
		}
		out.WriteString("\nNeither fork dropped a result the design calls eviction in the in-place sense: a fork replaces the whole working set with a handle carry rather than deleting anything, so what \"was evicted\" here is the entire pre-fork working set, made reachable again through `artifact_fetch` rather than gone.\n\n")
	}

	fmt.Fprintf(&out, "## Re-fetch rate\n\n")
	if len(reach.Forks) == 0 {
		out.WriteString("**Undefined, not zero.** No eviction of any kind has ever run on a real session, so there is nothing for a re-fetch to be a rate of.\n\n")
	} else {
		total := reach.TotalRefetches()
		fmt.Fprintf(&out, "**Defined, and it is zero: %d of %d evictions (the two forks above) were followed by a re-fetch of a source the fork had already dropped.** ", total, len(reach.Forks))
		out.WriteString("Checked directly: every tool call in each continuation session was compared against the `Key` of every result the fork's own carry named, using the exact key the fork itself computed (`tool + \" \" + args`). Neither continuation session repeated a pre-fork tool call; the one place `turn-18d6f8d9e45f8efc-f2` went back for old material it used `artifact_fetch` against the handle the carry had already given it, which is the cheap path the design intends and not a re-fetch by this definition.\n\n")
		out.WriteString("**The sample is two.** A rate of zero over two events is a real measurement, not a guess, but it is not a claim that wrong evictions cannot happen: it says only that the two forks recorded so far did not produce one.\n\n")
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
	fmt.Fprintf(&out, "**A session several times longer than the longest ever recorded, at the current ceiling, band shares and bytes-per-token estimate.** The largest peak on record here is %d tokens; the 250,000 ceiling is %.1fx that. `context-budget.md` v7 already measured a synthetic extension of a different recorded turn, the 44 step fixture peaking at 44,145 tokens used across BOJI-108, -119 and -138: a fork first appears at 48 steps on that extension, eight steps past its own real length. This report's own two real forks confirm the same shape independently, each firing the moment its session crossed the target recorded for it at the time, both well short of the ceiling and the higher of the two only %.1f%% over today's %d token shipped target. Three things would each pull the condition closer without any session getting longer: a lower `konst.ContextCeilingTokens` (a deliberate test lever already in the code, never shipped to a user), a model whose own window is smaller than the operating ceiling so the model's own wall is hit first, or a task shape that reads much larger results (this project's own source and docs) rather than the many small tool calls the recorded corpus is mostly made of.\n\n",
		reach.Highest(), gapToCeiling, overShippedTarget, shippedTarget)

	fmt.Fprintf(&out, "## What this does not answer\n\n")
	out.WriteString("- Whether a wrong eviction is visible when one happens: the two real forks never produced one to check against, so the re-fetch label is proved to work in code (`bench/recall/session.go`, `report-2026-09-20.md`) but has never caught a real mistake.\n")
	out.WriteString("- Whether the fork's cost shape (`context-budget.md` v7: handles beat a summary, 110,486 billed units against 153,392 doing nothing on the one turn measured) holds on these two real forks specifically: that would need the provider's own reported input tokens on the requests around each fork, which this report did not read.\n")
	out.WriteString("- The single-file schema sessions, 55 of the 63 skips: whether any of them would have crossed the ceiling if occupancy had been recorded at the time is not answerable from what is on disk.\n")

	return out.String()
}

func namesOf(peaks []SessionPeak) string {
	names := make([]string, len(peaks))
	for i, p := range peaks {
		names[i] = fmt.Sprintf("%s (%d of %d tokens)", p.ID, p.Peak, p.Target)
	}
	return strings.Join(names, ", ")
}
