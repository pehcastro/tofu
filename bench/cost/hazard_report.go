package cost

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"

	"tofu/bench/stat"
	"tofu/internal/judge/jev/wire/typesafe"
	"tofu/internal/judge/ledger"
)

const (
	beforeUSD       = 0.0000408
	beforeP50MS     = 346.0
	beforeP95MS     = 376.0
	beforeSource    = "before: $ from bench/wording/report-2026-09-18.md ($0.000734 over 18 calls, usage.cost, openrouter, tool_gate@1), ms from bench/api/report-2026-09-18.md (n=18, openrouter)"
	rerenderCommand = "go test ./bench/cost -run TestHazardReportRerendersFromItsAnswers -v"
	liveCommand     = "TOFU_LIVE_HAZARDS=1 go test ./bench/cost -run TestLiveHazardArmsOverTheHeldOutHalf -v"
)

type hazardMetric struct {
	name          string
	values        []float64
	before        float64
	lowerIsBetter bool
	rivals        []int
	format        func(float64) string
}

func renderHazards(scores []hazardScore, first hazardRow, answersPath string) string {
	regex, shipped, hazards := scores[0], scores[1], scores[2]
	none := math.NaN()
	count := func(v float64) string { return fmt.Sprintf("%.0f", v) }
	dollars := func(v float64) string { return fmt.Sprintf("$%.7f", v) }
	ms := func(v float64) string { return fmt.Sprintf("%.0f ms", v) }
	mean := func(values []float64) float64 {
		total := 0.0
		for _, v := range values {
			total += v
		}
		return total / float64(len(values))
	}
	accuracy := []int{0, 1}
	jevRivals := []int{1, 3}
	metrics := []hazardMetric{
		{fmt.Sprintf("correct of %d", regex.Cases), []float64{float64(regex.Correct), float64(shipped.Correct), float64(hazards.Correct)}, none, false, accuracy, count},
		{fmt.Sprintf("false blocks of %d benign", regex.Cases-regex.Blocks), []float64{float64(regex.FalseBlocks), float64(shipped.FalseBlocks), float64(hazards.FalseBlocks)}, none, true, accuracy, count},
		{fmt.Sprintf("blocks caught of %d dangerous", regex.Blocks), []float64{float64(regex.Caught), float64(shipped.Caught), float64(hazards.Caught)}, none, false, accuracy, count},
		{"dollars per decision", []float64{0, mean(shipped.USD), mean(hazards.USD)}, beforeUSD, true, jevRivals, dollars},
		{"p50 ms per decision", []float64{0, stat.Median(shipped.LatencyMS), stat.Median(hazards.LatencyMS)}, beforeP50MS, true, jevRivals, ms},
		{"p95 ms per decision", []float64{0, stat.Percentile(shipped.LatencyMS, 95), stat.Percentile(hazards.LatencyMS, 95)}, beforeP95MS, true, jevRivals, ms},
		{"max ms per decision", []float64{0, slices.Max(shipped.LatencyMS), slices.Max(hazards.LatencyMS)}, none, true, jevRivals, ms},
		{"input tokens per decision", []float64{0, mean(shipped.InputTokens), mean(hazards.InputTokens)}, none, true, jevRivals, count},
	}

	b := &strings.Builder{}
	fmt.Fprintf(b, "# bench cost: tool_gate hazards, %s\n\n", first.Date)
	fmt.Fprintf(b, "tldr: %s gets %d of %d correct, %d of %d false blocks, %d of %d caught, %s and %s a decision; %s gets %d, %d, %d, %s and %s; the regex gets %d, %d, %d for free.\n\n",
		armHazards, hazards.Correct, hazards.Cases, hazards.FalseBlocks, hazards.Cases-hazards.Blocks, hazards.Caught, hazards.Blocks,
		dollars(mean(hazards.USD)), ms(stat.Median(hazards.LatencyMS)),
		armShipped, shipped.Correct, shipped.FalseBlocks, shipped.Caught, dollars(mean(shipped.USD)), ms(stat.Median(shipped.LatencyMS)),
		regex.Correct, regex.FalseBlocks, regex.Caught)
	fmt.Fprintf(b, "- conditions: %s, machine %s, credential kind %s, wire %s, build %s, %d held-out cases of bench/corpus/gate/split.json, one call at a time, arm order alternated per case, no call discarded as warm-up\n",
		first.Date, first.Machine, first.CredentialKind, first.Wire, buildsOf(shipped, hazards), regex.Cases)
	fmt.Fprintf(b, "- dollars: %s\n", chargeLine(shipped, hazards))
	for _, score := range scores {
		fmt.Fprintf(b, "- %s: %s\n", score.Arm, score.Rule)
	}
	fmt.Fprintf(b, "- rerender from the recorded answers: `%s`\n- spend again: `%s`\n\n", rerenderCommand, liveCommand)

	source := fmt.Sprintf("source: bench/cost/%s; %s", answersPath, beforeSource)
	var wins, losses []string
	for _, m := range metrics {
		line, won := metricLine(m)
		if won {
			wins = append(wins, line)
			continue
		}
		losses = append(losses, line)
	}
	header := fmt.Sprintf("| Metric | %s | %s | %s | before | %s against the best rival |\n|---|---|---|---|---|---|\n", armRegex, armShipped, armHazards, armHazards)
	for _, table := range []struct {
		title string
		lines []string
	}{{"Wins", wins}, {"Losses", losses}} {
		fmt.Fprintf(b, "## %s\n\n%s\n\n%s", table.title, source, header)
		for _, line := range table.lines {
			b.WriteString(line)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func metricLine(m hazardMetric) (string, bool) {
	cells := []string{"no call", m.format(m.values[1]), m.format(m.values[2]), ""}
	if slices.Contains(m.rivals, 0) {
		cells[0] = m.format(m.values[0])
	}
	if !math.IsNaN(m.before) {
		cells[3] = m.format(m.before)
	}
	all := append(slices.Clone(m.values), m.before)
	names := []string{armRegex, armShipped, armHazards, "before"}
	best := -1
	for _, i := range m.rivals {
		if math.IsNaN(all[i]) {
			continue
		}
		if best < 0 || (m.lowerIsBetter && all[i] < all[best]) || (!m.lowerIsBetter && all[i] > all[best]) {
			best = i
		}
	}
	gap := m.values[2] - all[best]
	if m.lowerIsBetter {
		gap = -gap
	}
	outcome, won := "ties "+names[best], true
	switch {
	case m.format(m.values[2]) == m.format(all[best]):
	case gap > 0:
		outcome = fmt.Sprintf("beats %s by %s", names[best], m.format(gap))
	default:
		outcome, won = fmt.Sprintf("loses to %s by %s", names[best], m.format(-gap)), false
	}
	return fmt.Sprintf("| %s | %s | %s |\n", m.name, strings.Join(cells, " | "), outcome), won
}

func buildsOf(scores ...hazardScore) string {
	builds := map[string]int{}
	for _, score := range scores {
		maps.Copy(builds, score.Builds)
	}
	return strings.Join(slices.Sorted(maps.Keys(builds)), ", ")
}

func chargeLine(scores ...hazardScore) string {
	calls := map[ledger.ChargeSource]int{}
	for _, score := range scores {
		for source, n := range score.Charges {
			calls[source] += n
		}
	}
	return fmt.Sprintf("%d calls priced from usage.cost in the response, %d at list price because the response carried no cost, $%.3f per million input tokens with output free (typesafe.DollarsPerInputToken, docs.typesafe.ai/models.md), %d unpriced",
		calls[ledger.ChargeProvider], calls[ledger.ChargeList], typesafe.DollarsPerInputToken*1e6, calls[ledger.ChargeNone])
}
