package cost

import (
	"fmt"
	"strings"
	"time"

	"boji/bench/report"
)

func Render(result Result, conditions report.Conditions) string {
	b := &strings.Builder{}
	fmt.Fprintf(b, "# bench cost: %s\n\n", conditions.Date)
	fmt.Fprintf(b, "Machine: %s. Credential kind: %s. Wire: %s.\n\n", conditions.Machine, conditions.CredentialKind, conditions.Wire)
	fmt.Fprintf(b, "Three paid arms (jev, opus, fable) times six cases is 18 billed calls; the regex arm adds six more that cost nothing. Every figure below is from the response of the call it names, none computed from a price table.\n\n")

	renderLabels(b)
	renderArms(b, result)
	renderCostPerCorrect(b, result)
	renderContextAvoided(b, result)
	renderDisagreements(b, result)
	renderSpend(b, result)

	return b.String()
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
		fmt.Fprintf(b, "## Arm: %s\n\n", arm.Arm)
		b.WriteString("| Case | Model id | Input tokens | Output tokens | Dollars | Wall clock | Answer | Label | Correct |\n|---|---|---|---|---|---|---|---|---|\n")
		for _, c := range arm.Cases {
			fmt.Fprintf(b, "| %s | %s | %d | %d | $%.6f | %.0f ms | %s | %s | %v |\n",
				c.Case, c.ModelID, c.InputTokens, c.OutputTokens, c.Cost, c.LatencyMS, c.Verdict, c.Label, c.Correct)
		}
		b.WriteString("\n")
	}
}

func renderCostPerCorrect(b *strings.Builder, result Result) {
	b.WriteString("## Cost per correct decision\n\n")
	b.WriteString("| Arm | Correct | Total dollars | Dollars per correct decision |\n|---|---|---|---|\n")
	for _, arm := range result.Arms {
		if !arm.HasCostPerCorrect {
			fmt.Fprintf(b, "| %s | %d/%d | $%.6f | undefined, 0 correct |\n", arm.Arm, arm.CorrectCount, len(arm.Cases), arm.TotalCost)
			continue
		}
		fmt.Fprintf(b, "| %s | %d/%d | $%.6f | $%.6f |\n", arm.Arm, arm.CorrectCount, len(arm.Cases), arm.TotalCost, arm.CostPerCorrect)
	}
	b.WriteString("\nAn arm that is cheap and wrong divides its cost by a smaller correct count, or by zero, and loses on this column even when its raw dollar total is the lowest in the table.\n\n")
}

func renderContextAvoided(b *strings.Builder, result Result) {
	b.WriteString("## Context tokens avoided\n\n")
	fmt.Fprintf(b, "Sum of billed input tokens across the jev arm's six calls, the state plus the question set a planning model would otherwise have carried in its own context to make these six calls itself: %d tokens.\n\n", result.ContextTokensAvoided)
	fmt.Fprintf(b, "Frontier rate, measured from this run's own %s and %s calls rather than a price table: $%.8f over %d billed input tokens combined = $%.10f per token.\n\n",
		ModelOpus, ModelFable, result.FrontierRateFromCost, result.FrontierRateFromTokens, result.FrontierRatePerToken)
	fmt.Fprintf(b, "Arithmetic: %d tokens x $%.10f/token = $%.6f avoided at the frontier rate.\n\n", result.ContextTokensAvoided, result.FrontierRatePerToken, result.ContextDollarsAvoided)
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
	fmt.Fprintf(b, "## Total spend\n\nThis run cost $%.6f real dollars, summed from `usage.cost` (or its OpenRouter chat equivalent) on every arm.\n\n", result.TotalSpend)
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
