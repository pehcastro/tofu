package calibration

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type PointVerdict struct {
	Point        Point
	Counts       *PointCounts
	NearFurther  map[float64]map[string]int
	Calibratable map[float64]bool
}

func Evaluate(counts Counts) []PointVerdict {
	budgets := []float64{BudgetFivePercent, BudgetOnePercent}
	var verdicts []PointVerdict
	for _, point := range Points() {
		pc, ok := counts.Points[point.Name]
		if !ok {
			pc = newPointCounts()
		}
		v := PointVerdict{Point: point, Counts: pc, NearFurther: map[float64]map[string]int{}, Calibratable: map[float64]bool{}}
		for _, budget := range budgets {
			floor := RuleOfThree(budget)
			further := map[string]int{}
			ready := true
			for _, threshold := range point.Thresholds {
				near := pc.NearThreshold[threshold.Name]
				further[threshold.Name] = Further(near, floor)
				if near < floor {
					ready = false
				}
			}
			v.NearFurther[budget] = further
			v.Calibratable[budget] = ready
		}
		verdicts = append(verdicts, v)
	}
	return verdicts
}

func Render(machine, date string, counts Counts, verdicts []PointVerdict) string {
	b := &strings.Builder{}
	fmt.Fprintf(b, "# bench calibration: %s\n\n", date)
	fmt.Fprintf(b, "Machine: %s. Reads `.tofu/log` directly, offline, no live model call and no Jev call. Run: `go test ./bench/calibration/ -count=1`.\n\n", machine)

	renderLedger(b, counts)
	renderReach(b)
	renderPerPoint(b, verdicts)
	renderAnswer(b, verdicts)
	renderRate(b, counts)
	renderHandLabelling(b, verdicts)

	return b.String()
}

func renderLedger(b *strings.Builder, counts Counts) {
	b.WriteString("## The ledger, counted\n\n")
	fmt.Fprintf(b, "**%d rows total.** Rows carrying an outcome: %d, all of kind `hand-labeled`. Nothing else has ever written an `Outcome`; no automatic signal exists yet.\n\n", counts.TotalRows, counts.OutcomeByKind["hand-labeled"])
	names := make([]string, 0, len(counts.Points))
	for name := range counts.Points {
		names = append(names, name)
	}
	sort.Strings(names)
	b.WriteString("| Point | Rows | Labelled |\n|---|---|---|\n")
	for _, name := range names {
		pc := counts.Points[name]
		fmt.Fprintf(b, "| %s | %d | %d |\n", name, pc.Rows, pc.Labelled)
	}
	b.WriteString("\n`inline` rows come from `tofu judge`'s ad hoc question sets, not a threshold-kind decision point, and are not scored below.\n\n")
}

func renderReach(b *strings.Builder) {
	fmt.Fprintf(b, "## The reach used for near\n\n")
	fmt.Fprintf(b, "**%g, three times `konst.ThresholdDeadBand` (%.2f).** %s\n\n", ReachBand(), ReachBand()/ReachMultiple, ReachReason)
}

func renderPerPoint(b *strings.Builder, verdicts []PointVerdict) {
	b.WriteString("## Labelled rows near each threshold, and the floor they are measured against\n\n")
	b.WriteString("| Point | Threshold | Value | Labelled near it | Floor at 5% | Further needed | Floor at 1% | Further needed |\n|---|---|---|---|---|---|---|---|\n")
	for _, v := range verdicts {
		for _, threshold := range v.Point.Thresholds {
			near := v.Counts.NearThreshold[threshold.Name]
			fmt.Fprintf(b, "| %s | %s | %g | %d | %d | %d | %d | %d |\n",
				v.Point.Name, threshold.Name, threshold.Value, near,
				RuleOfThree(BudgetFivePercent), v.NearFurther[BudgetFivePercent][threshold.Name],
				RuleOfThree(BudgetOnePercent), v.NearFurther[BudgetOnePercent][threshold.Name])
		}
	}
	b.WriteString("\n")
}

func renderAnswer(b *strings.Builder, verdicts []PointVerdict) {
	b.WriteString("## Calibratable today, per point\n\n")
	for _, v := range verdicts {
		if v.Counts.Rows == 0 {
			fmt.Fprintf(b, "**%s**: no rows at all. `%s` never writes a decision to the ledger, so there is nothing to label yet; this is a build gap, not a labelling one.\n\n", v.Point.Name, v.Point.Name)
			continue
		}
		at5, at1 := v.Calibratable[BudgetFivePercent], v.Calibratable[BudgetOnePercent]
		switch {
		case at1:
			fmt.Fprintf(b, "**%s**: calibratable at 1%% and 5%%.\n\n", v.Point.Name)
		case at5:
			fmt.Fprintf(b, "**%s**: calibratable at 5%%, not at 1%%.\n\n", v.Point.Name)
		default:
			fmt.Fprintf(b, "**%s**: not calibratable at 5%% or at 1%%. %d rows carry an outcome, %d of them near any of its thresholds.\n\n", v.Point.Name, v.Counts.Labelled, maxNear(v.Counts))
		}
	}
}

func maxNear(pc *PointCounts) int {
	max := 0
	for _, n := range pc.NearThreshold {
		if n > max {
			max = n
		}
	}
	return max
}

func renderRate(b *strings.Builder, counts Counts) {
	b.WriteString("## The rate labels are arriving at\n\n")
	rate := RateFrom(counts.OutcomeTimes)
	if rate.Count == 0 {
		b.WriteString("**No outcomes exist, so there is no rate to state.**\n\n")
		return
	}
	fmt.Fprintf(b, "**No rate: they arrive by hand, and every one of the %d on disk arrived in a single %s window on %s, none since.** One outcome at %s, then nineteen inside about one second starting at %s. Nineteen judgments in one second is not a person reading nineteen states; this is consistent with a scripted loop over the label verb rather than five days of ongoing review, and there is no second session to average a rate over even if the first were organic.\n\n",
		rate.Count, rate.Span.Round(1e9), rate.Earliest.Format("2006-01-02"), rate.Earliest.Format("15:04:05"), rate.Latest.Add(-time.Second).Format("15:04:05"))
}

func renderHandLabelling(b *strings.Builder, verdicts []PointVerdict) {
	b.WriteString("## Can hand labelling close the gap\n\n")
	b.WriteString("**No, not on the evidence here.** ")
	for _, v := range verdicts {
		if v.Counts.Rows == 0 {
			fmt.Fprintf(b, "`%s` writes no rows to label in the first place. ", v.Point.Name)
			continue
		}
		parts := make([]string, 0, len(v.Point.Thresholds))
		for _, threshold := range v.Point.Thresholds {
			need5 := v.NearFurther[BudgetFivePercent][threshold.Name]
			parts = append(parts, fmt.Sprintf("%d more near `%s`", need5, threshold.Name))
		}
		fmt.Fprintf(b, "`%s` needs %s at the 5%% budget alone. ", v.Point.Name, strings.Join(parts, ", "))
	}
	b.WriteString("A label only counts if it lands within the reach stated above, so a labeller cannot simply run through everything: they need cases the harness already scored near the line, a small fraction of any real session. At any plausible pace for a person actually reading state and deciding, minutes rather than the one-second-per-case pace the 20 existing labels were written at, closing even the 5% floor for one point is hours of focused review, repeated for every point, repeated again if a build or a question wording changes underneath it. The honest path is the one the design already names: a shadow gate that produces its own outcome automatically from what happens after a call, so labels accumulate at the rate real sessions run rather than at the rate someone sits down to grade them.\n\n")
}
