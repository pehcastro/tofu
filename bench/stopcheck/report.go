package stopcheck

import (
	"fmt"
	"runtime"
	"strings"

	"tofu/internal/judge/jev/wire/typesafe"
	"tofu/internal/judge/ledger"
)

type Agreement struct {
	Arm           string
	Agreed        int
	Total         int
	AgreedOnStop  int
	LabelledStop  int
	AgreedOnRunOn int
	LabelledRunOn int
}

func (a Agreement) Line() string {
	if a.Total == 0 {
		return fmt.Sprintf("%s: no labelled steps", a.Arm)
	}
	return fmt.Sprintf("%s: %d of %d labelled steps, %.1f%%. of the %d steps labelled stop it caught %d, of the %d labelled continue it left %d alone",
		a.Arm, a.Agreed, a.Total, 100*float64(a.Agreed)/float64(a.Total),
		a.LabelledStop, a.AgreedOnStop, a.LabelledRunOn, a.AgreedOnRunOn)
}

func (a *Agreement) count(answer, label Answer) {
	a.Total++
	if label == Stop {
		a.LabelledStop++
	} else {
		a.LabelledRunOn++
	}
	if answer != label {
		return
	}
	a.Agreed++
	if label == Stop {
		a.AgreedOnStop++
		return
	}
	a.AgreedOnRunOn++
}

func (r Result) Agreements() (cheap, typed, chained Agreement) {
	return agreements(r.Steps)
}

var unreadOn20260921 = map[string]bool{"turn-18d68bcceb3d56e8": true, "turn-18d68bcff15ad87c": true}

func decidedOn20260921(steps []StepResult) []StepResult {
	var kept []StepResult
	for _, step := range steps {
		if !unreadOn20260921[step.Turn] {
			kept = append(kept, step)
		}
	}
	return kept
}

func agreements(steps []StepResult) (cheap, typed, chained Agreement) {
	cheap, typed, chained = Agreement{Arm: "cheap arm"}, Agreement{Arm: "typed arm"}, Agreement{Arm: "chained arm"}
	for _, step := range steps {
		cheap.count(step.Cheap.Answer, step.Label.Want)
		typed.count(step.Typed, step.Label.Want)
		chained.count(step.Chain.Answer, step.Label.Want)
	}
	return cheap, typed, chained
}

const RunCommand = "TOFU_LIVE=1 GOMAXPROCS=4 go test ./bench/stopcheck -run TestLiveBatteryOverEveryRecordedStep -v"

type before struct {
	agreed, stops string
}

var beforeOf = map[string]before{
	"cheap arm": {"68 of 78", "14 of 15"},
	"typed arm": {"71 of 78", "11 of 15"},
}

func armsTable(b *strings.Builder, previous map[string]before, steps []StepResult) {
	cheap, typed, chained := agreements(steps)
	b.WriteString("| arm | agreed | before | stops caught | before |\n|---|---|---|---|---|\n")
	for _, a := range []Agreement{cheap, typed, chained} {
		was, ok := previous[a.Arm]
		if !ok {
			was = before{"none", "none"}
		}
		fmt.Fprintf(b, "| %s | %d of %d | %s | %d of %d | %s |\n", a.Arm, a.Agreed, a.Total, was.agreed, a.AgreedOnStop, a.LabelledStop, was.stops)
	}
}

func chainSection(b *strings.Builder, r Result) {
	var taken, chainFresh, typedFresh int
	var typedCost, chainCost float64
	var typedMS, chainMS int64
	choices := map[string]int{}
	for _, step := range r.Steps {
		typedCost += step.Cost
		typedMS += step.LatencyMS
		if !step.Replayed {
			typedFresh++
		}
		if !step.Chain.Taken {
			continue
		}
		taken++
		choices[step.Chain.Choice]++
		chainCost += step.Chain.Cost
		chainMS += step.Chain.LatencyMS
		if !step.Chain.Replayed {
			chainFresh++
		}
	}
	steps := len(r.Steps)
	if steps == 0 {
		b.WriteString("no labelled steps\n\n")
		return
	}
	fmt.Fprintf(b, "steps that took the second request: %d of %d, %.1f%%. it chose %s %d, %s %d, %s %d\n\n",
		taken, steps, 100*float64(taken)/float64(steps), nextContinue, choices[nextContinue], nextAsk, choices[nextAsk], nextStop, choices[nextStop])
	b.WriteString("| arm | dollars per step | wall ms per step | fresh calls this run |\n|---|---|---|---|\n")
	fmt.Fprintf(b, "| typed arm, one call | $%.6f | %d | %d of %d |\n", typedCost/float64(steps), typedMS/int64(steps), typedFresh, steps)
	fmt.Fprintf(b, "| chained arm | $%.6f | %d | %d of %d, plus %d of %d second requests |\n\n",
		(typedCost+chainCost)/float64(steps), (typedMS+chainMS)/int64(steps), typedFresh, steps, chainFresh, taken)
	fmt.Fprintf(b, "wall ms is the sum of the requests a step sent, one after the other, divided by every labelled step. no call was discarded as a warm up. a replayed answer carries the latency and cost measured when it was first bought.\n\n")
}

func costLine(r Result) string {
	charged := map[ledger.ChargeSource]int{}
	for _, step := range r.Steps {
		if !step.Replayed {
			charged[step.Charge]++
		}
		if step.Chain.Taken && !step.Chain.Replayed {
			charged[step.Chain.Charge]++
		}
	}
	fresh := charged[ledger.ChargeProvider] + charged[ledger.ChargeList] + charged[ledger.ChargeNone]
	return fmt.Sprintf("Cost unit: money, in USD. of this run's %d fresh calls, %d carry a cost the provider read back in its response, %d are priced at list rate because the response carried none, and %d have no price. the list rate is %.0f USD per billion input tokens, typesafe.DollarsPerInputToken in internal/judge/jev/wire/typesafe/typesafe.go, which the jev client applies through ledger.ChargeOf; output tokens are not priced. a replayed answer carries the figure from when it was first bought.",
		fresh, charged[ledger.ChargeProvider], charged[ledger.ChargeList], charged[ledger.ChargeNone], typesafe.DollarsPerInputToken*1e9)
}

func winner(cheap, typed Agreement) string {
	lead := fmt.Sprintf("neither arm won, both agreed with the hand labels on %d steps", typed.Agreed)
	if cheap.Agreed > typed.Agreed {
		lead = fmt.Sprintf("the cheap arm won, %d agreements against %d, so a typed decision costing money and latency read the turn worse than plain string work did", cheap.Agreed, typed.Agreed)
	}
	if typed.Agreed > cheap.Agreed {
		lead = fmt.Sprintf("the typed arm won, %d agreements against %d", typed.Agreed, cheap.Agreed)
	}
	verdict := "both arms caught every step labelled stop"
	if cheap.AgreedOnStop < cheap.LabelledStop || typed.AgreedOnStop < typed.LabelledStop {
		verdict = "neither arm is fit to end a turn, and the point stays in shadow"
	}
	return fmt.Sprintf("%s out of %d labelled steps, which on a corpus this size is one labeller's judgement on a handful of steps rather than a result. the class that decides it is stop: the cheap arm caught %d of %d and the typed arm caught %d of %d, so %s.",
		lead, typed.Total, cheap.AgreedOnStop, cheap.LabelledStop, typed.AgreedOnStop, typed.LabelledStop, verdict)
}

func Render(r Result) string {
	var b strings.Builder
	cheap, typed, chained := r.Agreements()
	fmt.Fprintf(&b, "# stop_check, the second decision point\n\n")
	fmt.Fprintf(&b, "generated %s, jev build %s, wire typesafe, credential kind api key, machine %s/%s, questions stop_check@%d, rule mode %s (%s)\n\n",
		r.GeneratedAt.UTC().Format("2006-01-02 15:04:05Z"), r.Build, runtime.GOOS, runtime.GOARCH, r.Wording, r.Mode, r.ModeReason)
	fmt.Fprintf(&b, "%s\n\n", costLine(r))
	fmt.Fprintf(&b, "run it again with `%s`. it needs the network and the TypeSafe key, and skips without TOFU_LIVE. a test home is made per process, so every run asks fresh and spends again; identical states inside one run replay from that run's cache.\n\n", RunCommand)

	b.WriteString(Summary(r))

	fmt.Fprintf(&b, "## Which arm won\n\n")
	fmt.Fprintf(&b, "%s\n\n%s\n%s\n%s\n\n", winner(cheap, typed), cheap.Line(), typed.Line(), chained.Line())
	b.WriteString(details(r))
	return b.String()
}

func Summary(r Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Three arms\n\n")
	b.WriteString("the cheap arm is the repeat check, the typed arm is one stop_check@1 call as shipped, the chained arm sends a second request with the brief and the last three tool results when the first is unsure: stalled and work_remains both 0.5 or more, or stop_pressure within the dead band of the ask threshold. the before columns are bench/stopcheck/report-2026-09-21.md. the shared reader now reads turn-18d68bcceb3d56e8 and turn-18d68bcff15ad87c, which that run skipped, so the first table is the 78 steps it decided and the second is every labelled step.\n\n")
	armsTable(&b, beforeOf, decidedOn20260921(r.Steps))
	b.WriteString("\n")
	armsTable(&b, nil, r.Steps)
	b.WriteString("\n")

	fmt.Fprintf(&b, "## What the chain costs\n\n")
	chainSection(&b, r)
	return b.String()
}

func details(r Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Corpus\n\n")
	fmt.Fprintf(&b, "labelled steps: %d\nlabel source: %s\n", len(r.Steps), LabelSource)
	fmt.Fprintf(&b, "turns with no steps, which offer no decision point: %d%s\n", len(r.TurnsWithoutSteps), namesOf(r.TurnsWithoutSteps))
	fmt.Fprintf(&b, "session files skipped: %d\n", len(r.Skipped))
	for _, s := range r.Skipped {
		fmt.Fprintf(&b, "  %s: %s\n", s.Path, s.Reason)
	}
	if len(r.Unlabelled) > 0 {
		fmt.Fprintf(&b, "steps with no hand label, not decided and not counted: %d%s\n", len(r.Unlabelled), namesOf(r.Unlabelled))
	}
	b.WriteString("\n")

	fmt.Fprintf(&b, "## Disagreements, named\n\n")
	wrote := false
	for _, step := range r.Steps {
		if step.Cheap.Answer == step.Label.Want && step.Typed == step.Label.Want && step.Chain.Answer == step.Label.Want {
			continue
		}
		wrote = true
		fmt.Fprintf(&b, "%s step %d: label %s, because %s\n", step.Turn, step.Step, step.Label.Want, step.Label.Note)
		fmt.Fprintf(&b, "  cheap arm said %s, because %s\n", step.Cheap.Answer, step.Cheap.Rule)
		fmt.Fprintf(&b, "  typed arm said %s, verdict %s, stop_pressure %.2f stalled %.2f work_remains %.2f budget_exhausted %.2f, row %s\n",
			step.Typed, step.Verdict, step.Answers["stop_pressure"], step.Answers["stalled"],
			step.Answers["work_remains"], step.Answers["budget_exhausted"], step.RowID)
		fmt.Fprintf(&b, "  chained arm said %s%s\n", step.Chain.Answer, chainNote(step.Chain))
	}
	if !wrote {
		b.WriteString("neither arm disagreed with a label\n")
	}
	b.WriteString("\n")

	fmt.Fprintf(&b, "## What the second point costs per turn\n\n")
	fmt.Fprintf(&b, "gate figures come from the ledger rows the tool gate wrote while these turns ran, so the stop check is measured against a gate that was already running, not against an empty turn.\n\n")
	var gateCost, stopCost float64
	var gateLatency, stopLatency int64
	for _, turn := range r.Turns {
		fmt.Fprintf(&b, "%s: %d steps, gate %d decisions %d ms $%.6f, stop_check %d decisions %d ms $%.6f, %d of them called fresh this run",
			turn.Turn, turn.Steps, turn.GateDecisions, turn.GateLatencyMS, turn.GateCost,
			turn.StopDecisions, turn.StopLatencyMS, turn.StopCost, turn.StopFresh)
		if turn.GateMissing > 0 {
			fmt.Fprintf(&b, ", %d gate decisions not in the ledger", turn.GateMissing)
		}
		b.WriteString("\n")
		gateCost += turn.GateCost
		stopCost += turn.StopCost
		gateLatency += turn.GateLatencyMS
		stopLatency += turn.StopLatencyMS
	}
	fresh := 0
	for _, turn := range r.Turns {
		fresh += turn.StopFresh
	}
	turns := len(r.Turns)
	if turns > 0 {
		fmt.Fprintf(&b, "\nper turn, mean over %d turns: gate %d ms $%.6f, stop_check adds %d ms $%.6f\n",
			turns, gateLatency/int64(turns), gateCost/float64(turns), stopLatency/int64(turns), stopCost/float64(turns))
		fmt.Fprintf(&b, "over the whole corpus: gate %d ms $%.6f, stop_check %d ms $%.6f. %d of this run's decisions were fresh calls and the rest came from the replay cache, whose latency and cost are the ones measured when the answer was first bought\n",
			gateLatency, gateCost, stopLatency, stopCost, fresh)
	}
	b.WriteString("\n")

	fmt.Fprintf(&b, "## Every step\n\n")
	for _, step := range r.Steps {
		fmt.Fprintf(&b, "%s step %d: label %s, cheap %s, typed %s (%s), chained %s%s, row %s\n",
			step.Turn, step.Step, step.Label.Want, step.Cheap.Answer, step.Typed, step.Verdict, step.Chain.Answer, chainNote(step.Chain), step.RowID)
	}
	return b.String()
}

func chainNote(c Chain) string {
	if !c.Taken {
		return ", the first answer was sure so no second request"
	}
	return fmt.Sprintf(", second request chose %s, row %s", c.Choice, c.RowID)
}

func namesOf(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return ": " + strings.Join(names, ", ")
}
