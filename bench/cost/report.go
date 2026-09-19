package cost

import (
	"fmt"
	"strings"
	"time"

	"boji/bench/report"
	"boji/internal/judge/ledger"
)

func Render(result Result, conditions report.Conditions) string {
	b := &strings.Builder{}
	fmt.Fprintf(b, "# bench cost: %s\n\n", conditions.Date)
	fmt.Fprintf(b, "Machine: %s. Credential kind: %s. Wire: %s.\n\n", conditions.Machine, conditions.CredentialKind, conditions.Wire)
	units := make([]ledger.Unit, 0, len(result.Arms))
	for _, arm := range result.Arms {
		units = append(units, arm.Unit)
	}
	fmt.Fprintf(b, "%s\n\n", report.CostUnitLine(units))
	fmt.Fprintf(b, "Three model arms (jev, opus, fable) times six cases is 18 calls that spend something; the regex arm adds six more that spend nothing. Every figure below is from the response of the call it names, none computed from a price table.\n\n")

	renderHeadline(b, result)
	renderLabels(b)
	renderArms(b, result)
	renderCostPerCorrect(b, result)
	renderContextAvoided(b, result)
	renderDisagreements(b, result)
	renderSpend(b, result)

	return b.String()
}

func renderHeadline(b *strings.Builder, result Result) {
	h := result.Headline
	b.WriteString("## Headline\n\n")
	if h.HasRatio {
		fmt.Fprintf(b, "Jev decides for $%.6f per correct decision. The %s arm decides for $%.6f, %.0f times Jev. Both sides are money on the same credential kind, so the ratio is defined and the division is legal.\n\n",
			h.JevMoneyPerCorrect, h.FrontierArm, h.FrontierMoneyPerCorrect, h.Ratio)
		return
	}
	fmt.Fprintf(b, "There is no ratio to report. Jev costs $%.6f of money per correct decision. The %s arm's cost is %s, so dividing one by the other would divide dollars by something that is not dollars.\n\n",
		h.JevMoneyPerCorrect, h.FrontierArm, h.FrontierUnit)
	fmt.Fprintf(b, "What survives the split is a comparison in a unit both arms report: %d billed input tokens per correct decision for jev against %d for %s, and the wall clock in the tables below. %s\n\n",
		h.JevTokensPerCorrect, h.FrontierTokensPerCorrect, h.FrontierArm, ledger.UnitsDoNotAdd)
}

func renderLabels(b *strings.Builder) {
	b.WriteString("## Labels\n\n")
	fmt.Fprintf(b, "Every case shares the same label source: %s\n\n", labelSource)
	b.WriteString("| Case | Label |\n|---|---|\n")
	for _, name := range caseOrder() {
		fmt.Fprintf(b, "| %s | %s |\n", name, Labels[name].Verdict)
	}
	b.WriteString("\n")
}

func renderArms(b *strings.Builder, result Result) {
	for _, arm := range result.Arms {
		fmt.Fprintf(b, "## Arm: %s, spending %s\n\n", arm.Arm, arm.Unit)
		b.WriteString("| Case | Model id | Input tokens | Output tokens | Money | List price | Wall clock | Answer | Label | Correct |\n|---|---|---|---|---|---|---|---|---|---|\n")
		for _, c := range arm.Cases {
			fmt.Fprintf(b, "| %s | %s | %d | %d | %s | %s | %.0f ms | %s | %s | %v |\n",
				c.Case, c.ModelID, c.InputTokens, c.OutputTokens,
				unitCell(arm.Unit, ledger.UnitMoney, float64(c.Money)), unitCell(arm.Unit, ledger.UnitListPrice, float64(c.List)),
				c.LatencyMS, c.Verdict, c.Label, c.Correct)
		}
		b.WriteString("\n")
	}
}

func renderCostPerCorrect(b *strings.Builder, result Result) {
	b.WriteString("## Cost per correct decision\n\n")
	b.WriteString("| Arm | Unit | Correct | Total money | Total list price | Unpriced calls | Money per correct | List price per correct |\n|---|---|---|---|---|---|---|---|\n")
	for _, arm := range result.Arms {
		fmt.Fprintf(b, "| %s | %s | %d/%d | %s | %s | %d | %s | %s |\n",
			arm.Arm, arm.Unit, arm.CorrectCount, len(arm.Cases),
			unitCell(arm.Unit, ledger.UnitMoney, float64(arm.Total.Money)),
			unitCell(arm.Unit, ledger.UnitListPrice, float64(arm.Total.List)),
			arm.Total.UnpricedCalls,
			perCorrectCell(arm, ledger.UnitMoney, float64(arm.MoneyPerCorrect)),
			perCorrectCell(arm, ledger.UnitListPrice, float64(arm.ListPerCorrect)))
	}
	b.WriteString("\nThe two cost columns are never summed and no column here totals across arms of different units. An arm that is cheap and wrong divides its cost by a smaller correct count, or by zero, and loses on its own unit's column even when its raw total is the lowest in the table.\n\n")
}

func renderContextAvoided(b *strings.Builder, result Result) {
	b.WriteString("## Context tokens avoided\n\n")
	fmt.Fprintf(b, "Sum of billed input tokens across the jev arm's six calls, the state plus the question set a planning model would otherwise have carried in its own context to make these six calls itself: %d tokens.\n\n", result.ContextTokensAvoided)
	if result.FrontierUnit != ledger.UnitMoney {
		fmt.Fprintf(b, "Those tokens carry no dollar figure in this run: the frontier arms' cost is %s, so there is no measured rate per token to multiply by, and this project has no price table to invent one from. The avoided quantity is %d tokens and stays in tokens.\n\n",
			result.FrontierUnit, result.ContextTokensAvoided)
		return
	}
	fmt.Fprintf(b, "Frontier rate, measured from this run's own %s and %s calls rather than a price table: $%.8f over %d billed input tokens combined = $%.10f per token.\n\n",
		ModelOpus, ModelFable, result.FrontierRateFromMoney, result.FrontierRateFromTokens, result.FrontierRatePerToken)
	fmt.Fprintf(b, "Arithmetic: %d tokens x $%.10f/token = $%.6f of money avoided at the frontier rate.\n\n", result.ContextTokensAvoided, result.FrontierRatePerToken, result.ContextDollarsAvoided)
}

func renderDisagreements(b *strings.Builder, result Result) {
	b.WriteString("## Regex arm against the label\n\n")
	if len(result.RegexMatchesLabel) == 0 {
		b.WriteString("The regex arm matched the label on no case.\n\n")
	} else {
		fmt.Fprintf(b, "The regex arm matched the label on: %s.\n\n", strings.Join(result.RegexMatchesLabel, ", "))
	}

	b.WriteString("## Disagreements\n\n")
	if len(result.Disagreements) == 0 {
		b.WriteString("No arm disagreed with its label on any case.\n\n")
		return
	}
	for _, d := range result.Disagreements {
		fmt.Fprintf(b, "### %s, case %s\n\n", d.Arm, d.Case)
		fmt.Fprintf(b, "State:\n\n```json\n%s\n```\n\n", d.State)
		if d.Answer == Refused {
			fmt.Fprintf(b, "Question: user_requested and approval feed the decision, and this arm refused to answer them: %q\n\n", d.Refusal)
		} else {
			fmt.Fprintf(b, "Question: user_requested and approval feed the decision. user_requested = %.2f, approval = %.2f.\n\n", d.UserRequested, d.Approval)
		}
		fmt.Fprintf(b, "Answer: %s. Label: %s, from %s.\n\n", d.Answer, d.Label, d.LabelSource)
	}
}

func renderSpend(b *strings.Builder, result Result) {
	b.WriteString("## Total spend\n\n")
	b.WriteString("| Money | List price | Unpriced calls |\n|---|---|---|\n")
	fmt.Fprintf(b, "| $%.6f | $%.6f | %d |\n\n", result.Total.Money, result.Total.List, result.Total.UnpricedCalls)
	fmt.Fprintf(b, "Money is summed from `usage.cost` (or its OpenRouter chat equivalent) on every metered arm. %s\n\n", ledger.UnitsDoNotAdd)
}

func unitCell(arm, column ledger.Unit, value float64) string {
	if arm != column {
		return ""
	}
	return fmt.Sprintf("$%.6f", value)
}

func perCorrectCell(arm ArmResult, column ledger.Unit, value float64) string {
	if arm.Unit == column && arm.CorrectCount == 0 {
		return "undefined, 0 correct"
	}
	return unitCell(arm.Unit, column, value)
}

func caseOrder() []string {
	return []string{
		"case-1-ls.json",
		"case-2-force-push-tests.json",
		"case-3-force-push-requested.json",
		"case-4-rm-rf.json",
		"case-5-curl-exfil-planted.json",
		"case-6-sed-named-file.json",
	}
}

func Filename(now time.Time) string {
	return fmt.Sprintf("report-%s.md", now.Format("2006-01-02"))
}
