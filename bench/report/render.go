package report

import (
	"fmt"
	"strings"
	"time"

	"tofu/bench/api"
)

type Conditions struct {
	Machine        string
	CredentialKind string
	Wire           string
	Date           string
}

type baselineSize struct {
	label       string
	medianMS    float64
	billedInput int
}

var bench001Sizes = []baselineSize{
	{"300", 427, 544},
	{"1k", 459, 1184},
	{"4k", 440, 3824},
	{"12k", 577, 10944},
	{"28k", 769, 25184},
}

const (
	bench001GateMedianMS = 658.0
	bench001GateMaxMS    = 1625.0
	bench001Cost255Tok   = 6172.0
	bench001Cost255MS    = 577.0
	moveThresholdPct     = 20.0
)

func Render(result api.Result, conditions Conditions) string {
	b := &strings.Builder{}
	fmt.Fprintf(b, "# bench api: %s\n\n", conditions.Date)
	fmt.Fprintf(b, "Machine: %s. Credential kind: %s. Wire: %s. Build id the response reported: `%s`.\n\n",
		conditions.Machine, conditions.CredentialKind, conditions.Wire, result.Build)
	fmt.Fprintf(b, "No first call was discarded as a warm-up; every call below, including the first, counts.\n\n")

	renderSizeSection(b, result)
	renderCountSection(b, result)
	renderGateSection(b, result)
	renderRerunSection(b, result)
	renderSweepSection(b, result)
	renderCostSection(b, result)
	renderComparison(b, result)

	return b.String()
}

func renderSizeSection(b *strings.Builder, result api.Result) {
	b.WriteString("## Latency by state size\n\n")
	b.WriteString("| State | Runs | Median | p95 | p99 | Min | Max | Billed input tokens | Build |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|\n")
	for _, s := range result.SizeLatencies {
		fmt.Fprintf(b, "| %s | %d | %.0f ms | %.0f ms | %.0f ms | %.0f ms | %.0f ms | %v | `%s` |\n",
			s.Label, s.Runs, s.MedianMS, s.P95MS, s.P99MS, s.MinMS, s.MaxMS, s.BilledInput, s.Build)
	}
	b.WriteString("\np95 and p99 above rest on 3 runs per state size. A p99 from 3 runs is not a real p99, it is the max; treat it as directional only.\n\n")
}

func renderCountSection(b *strings.Builder, result api.Result) {
	b.WriteString("## Latency by question count\n\n")
	b.WriteString("| Questions | Runs | Median |\n|---|---|---|\n")
	for _, c := range result.CountLatencies {
		fmt.Fprintf(b, "| %d | %d | %.0f ms |\n", c.Count, c.Runs, c.MedianMS)
	}
	b.WriteString("\n")
}

func renderGateSection(b *strings.Builder, result api.Result) {
	b.WriteString("## Six-case gate battery\n\n")
	fmt.Fprintf(b, "n=%d calls (6 cases, 3 reps each). min %.0f ms (n=%d), p50 %.0f ms (n=%d), p95 %.0f ms (2nd-largest of %d, tail unmeasured), p99 %.0f ms (2nd-largest of %d, tail unmeasured), max %.0f ms (n=%d). bench-001's own tail, a live call and its retry both timing out at 2.5 s during the BOJI-005 review, needs far more than %d samples to reappear here; treat p95/p99 above as directional, not as the real tail.\n\n",
		result.GateSampleCount, result.GateMinMS, result.GateSampleCount, result.GateMedianMS, result.GateSampleCount,
		result.GateP95MS, result.GateSampleCount, result.GateP99MS, result.GateSampleCount, result.GateMaxMS, result.GateSampleCount, result.GateSampleCount)
	b.WriteString("| Case | risk | approval | user_requested | from_untrusted | Latency |\n|---|---|---|---|---|---|\n")
	for _, c := range result.GateCases {
		fmt.Fprintf(b, "| %s | %.2f | %.2f | %.2f | %.2f | %.0f ms |\n",
			c.Name, c.Answers["risk"], c.Answers["approval"], c.Answers["user_requested"], c.Answers["from_untrusted"], c.LatencyMS)
	}
	b.WriteString("\n")
}

func renderRerunSection(b *strings.Builder, result api.Result) {
	fmt.Fprintf(b, "## Rerun agreement, %d runs on `%s`\n\n", len(result.RerunResults[0].Values), result.RerunCase)
	b.WriteString("| Question | Values | Min | Max | Spread | Straddles 0.50 |\n|---|---|---|---|---|---|\n")
	var straddlers []string
	for _, r := range result.RerunResults {
		fmt.Fprintf(b, "| %s | %v | %.2f | %.2f | %.2f | %v |\n", r.ID, r.Values, r.Min, r.Max, r.Spread, r.Straddle)
		if r.Straddle {
			straddlers = append(straddlers, r.ID)
		}
	}
	if len(straddlers) > 0 {
		fmt.Fprintf(b, "\nStraddles 0.50 on both sides: %s. A threshold at 0.50 would fire inconsistently on identical requests for these.\n\n", strings.Join(straddlers, ", "))
	} else {
		b.WriteString("\nNo question straddled 0.50 across these reruns.\n\n")
	}
}

func renderSweepSection(b *strings.Builder, result api.Result) {
	b.WriteString("## Option sweep\n\n")
	b.WriteString("| Options | Result | Latency | Billed input tokens | Correct | Confidence | Cost |\n|---|---|---|---|---|---|---|\n")
	for _, s := range result.OptionSweep {
		if !s.Succeeded {
			fmt.Fprintf(b, "| %d | failed | | | | | | server message: %s |\n", s.Options, s.ServerError)
			continue
		}
		fmt.Fprintf(b, "| %d | ok | %.0f ms | %d | %v | %.2f | $%.6f |\n",
			s.Options, s.LatencyMS, s.BilledInput, s.Correct, s.Confidence, s.Cost)
	}
	b.WriteString("\n")
}

func renderCostSection(b *strings.Builder, result api.Result) {
	fmt.Fprintf(b, "## Cost\n\nTotal spend of this run: $%.6f over %d calls, taken from `usage.cost` on each response, not computed.\n\n",
		result.TotalCost, result.TotalCalls)
}

func renderComparison(b *strings.Builder, result api.Result) {
	b.WriteString("## Comparison against bench-001\n\n")
	moved := false
	for i, s := range result.SizeLatencies {
		if i >= len(bench001Sizes) {
			break
		}
		base := bench001Sizes[i]
		pct := movedPct(s.MedianMS, base.medianMS)
		if abs(pct) > moveThresholdPct {
			moved = true
			fmt.Fprintf(b, "- state size %s: median moved %.0f%% (%.0f ms -> %.0f ms against bench-001), n=%d runs here. ", s.Label, pct, base.medianMS, s.MedianMS, s.Runs)
			fmt.Fprintf(b, "Cause: not established, n=%d is too small to separate a real move from tail noise.\n", s.Runs)
		}
	}
	if pct := movedPct(result.GateMedianMS, bench001GateMedianMS); abs(pct) > moveThresholdPct {
		moved = true
		fmt.Fprintf(b, "- gate battery median moved %.0f%% (%.0f ms -> %.0f ms against bench-001's 658 ms), n=%d here. Cause: not established, n=%d is too small to separate a real move from tail noise.\n",
			pct, bench001GateMedianMS, result.GateMedianMS, result.GateSampleCount, result.GateSampleCount)
	}
	if pct := movedPct(result.GateMaxMS, bench001GateMaxMS); abs(pct) > moveThresholdPct {
		moved = true
		fmt.Fprintf(b, "- gate battery worst call moved %.0f%% (%.0f ms -> %.0f ms against bench-001's 1,625 ms), n=%d here against bench-001's 6. Cause: not established, this is exactly the tail neither run sampled enough to trust.\n",
			pct, bench001GateMaxMS, result.GateMaxMS, result.GateSampleCount)
	}
	for _, s := range result.OptionSweep {
		if s.Options != 255 || !s.Succeeded {
			continue
		}
		pctLatency := movedPct(s.LatencyMS, bench001Cost255MS)
		if abs(pctLatency) > moveThresholdPct {
			moved = true
			fmt.Fprintf(b, "- 255-option latency moved %.0f%% (%.0f ms -> %.0f ms against bench-001's 577 ms), n=1 call here. Cause: not established, a single call proves nothing about a real move.\n", pctLatency, bench001Cost255MS, s.LatencyMS)
		}
		pctTokens := movedPct(float64(s.BilledInput), bench001Cost255Tok)
		if abs(pctTokens) > moveThresholdPct {
			moved = true
			fmt.Fprintf(b, "- 255-option billed tokens moved %.0f%% (%.0f -> %d against bench-001's 6,172), n=1 call here. Cause known: this run's option names (item_0..item_254) are shorter than bench-001's original option text, so fewer tokens is expected, not a regression.\n", pctTokens, bench001Cost255Tok, s.BilledInput)
		}
	}
	if !moved {
		b.WriteString("No figure moved by more than 20% against bench-001.\n")
	}
	fmt.Fprintf(b, "\nNo baseline exists in bench-001 for: latency by question count (that finding reported flat, not the medians), rerun agreement on `%s`, and the 256-option point (bench-001's ceiling probe used 300, not 256), so these are reported without a delta.\n", result.RerunCase)
}

func movedPct(current, baseline float64) float64 {
	if baseline == 0 {
		return 0
	}
	return (current - baseline) / baseline * 100
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func Filename(prefix string, now time.Time) string {
	return fmt.Sprintf("%s-%s.md", prefix, now.Format("2006-01-02"))
}

func TimesTheMean(value, mean float64) string {
	if mean == 0 {
		return "unbounded against a mean of zero"
	}
	return fmt.Sprintf("%.1f times the mean", value/mean)
}
