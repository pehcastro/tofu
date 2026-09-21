package stopcheck

import (
	"fmt"
	"strings"

	"tofu/bench/report"
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

func (r Result) Agreements() (cheap, typed Agreement) {
	cheap, typed = Agreement{Arm: "cheap arm"}, Agreement{Arm: "typed arm"}
	for _, step := range r.Steps {
		cheap.count(step.Cheap.Answer, step.Label.Want)
		typed.count(step.Typed, step.Label.Want)
	}
	return cheap, typed
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
	cheap, typed := r.Agreements()
	fmt.Fprintf(&b, "# stop_check, the second decision point\n\n")
	fmt.Fprintf(&b, "generated %s, jev build %s, questions stop_check@%d, rule mode %s (%s)\n\n",
		r.GeneratedAt.UTC().Format("2006-01-02 15:04:05Z"), r.Build, r.Wording, r.Mode, r.ModeReason)
	fmt.Fprintf(&b, "%s\n\n", report.CostUnitLine([]ledger.Unit{ledger.UnitMoney}))

	fmt.Fprintf(&b, "## Which arm won\n\n")
	fmt.Fprintf(&b, "%s\n\n%s\n%s\n\n", winner(cheap, typed), cheap.Line(), typed.Line())

	fmt.Fprintf(&b, "## Corpus\n\n")
	fmt.Fprintf(&b, "labelled steps: %d\nlabel source: %s\n", len(r.Steps), LabelSource)
	fmt.Fprintf(&b, "turns with no steps, which offer no decision point: %d%s\n", len(r.TurnsWithoutSteps), namesOf(r.TurnsWithoutSteps))
	fmt.Fprintf(&b, "session files skipped: %d\n", len(r.Skipped))
	for _, s := range r.Skipped {
		fmt.Fprintf(&b, "  %s: %s\n", s.File, s.Why)
	}
	if len(r.Unlabelled) > 0 {
		fmt.Fprintf(&b, "steps with no hand label, not decided and not counted: %d%s\n", len(r.Unlabelled), namesOf(r.Unlabelled))
	}
	b.WriteString("\n")

	fmt.Fprintf(&b, "## Disagreements, named\n\n")
	wrote := false
	for _, step := range r.Steps {
		if step.Cheap.Answer == step.Label.Want && step.Typed == step.Label.Want {
			continue
		}
		wrote = true
		fmt.Fprintf(&b, "%s step %d: label %s, because %s\n", step.Turn, step.Step, step.Label.Want, step.Label.Note)
		fmt.Fprintf(&b, "  cheap arm said %s, because %s\n", step.Cheap.Answer, step.Cheap.Rule)
		fmt.Fprintf(&b, "  typed arm said %s, verdict %s, stop_pressure %.2f stalled %.2f work_remains %.2f budget_exhausted %.2f, row %s\n",
			step.Typed, step.Verdict, step.Answers["stop_pressure"], step.Answers["stalled"],
			step.Answers["work_remains"], step.Answers["budget_exhausted"], step.RowID)
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
		fmt.Fprintf(&b, "%s step %d: label %s, cheap %s, typed %s (%s), row %s\n",
			step.Turn, step.Step, step.Label.Want, step.Cheap.Answer, step.Typed, step.Verdict, step.RowID)
	}
	return b.String()
}

func namesOf(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return ": " + strings.Join(names, ", ")
}
