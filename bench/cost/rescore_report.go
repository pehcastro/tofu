package cost

import (
	"fmt"
	"strings"
)

const deterministicArmSource = "recomputed from the held-out half: these two arms read the state and ask no question, so no policy decides for them and nothing about them can change"

func RenderRescore(result RescoreResult) string {
	b := &strings.Builder{}
	fmt.Fprintf(b, "Model calls made by the re-score: %d. The three probabilistic arms come from %s and every row in it is marked recorded. The regex and always-proceed arms were %s\n\n", result.ModelCalls, recordedAnswersPath, deterministicArmSource)
	p := result.Policy
	fmt.Fprintf(b, "The gate is `%s`, resolved to %s: %s. %s asks at %.2f and denies at %.2f, %s relaxes below %.2f, %s relaxes above %.2f, %s blocks at %.2f.\n\n",
		p.File, result.Resolution.Mode, result.Resolution.Reason,
		p.RiskQuestion, p.Thresholds.RiskAskAt, p.Thresholds.RiskDenyAt,
		p.ApprovalQuestion, p.Thresholds.ApprovalRelaxAt,
		p.UserRequestedQuestion, p.Thresholds.UserRequestedRelaxAt,
		p.FromUntrustedQuestion, p.Thresholds.FromUntrustedBlockAt)

	b.WriteString("| Arm | Rows | Re-scored through the gate | Answers without risk | Verdict alone | Refusals | Verdict changed | Correct as recorded | Correct re-scored |\n|---|---|---|---|---|---|---|---|---|\n")
	totals := RescoreCount{Arm: "all"}
	for _, arm := range result.Arms {
		renderRescoreRow(b, arm)
		totals.Rows += arm.Rows
		totals.Rescored += arm.Rescored
		totals.PartialAnswers += arm.PartialAnswers
		totals.VerdictOnly += arm.VerdictOnly
		totals.Refusals += arm.Refusals
		totals.Changed += arm.Changed
		totals.CorrectRecorded += arm.CorrectRecorded
		totals.CorrectRescored += arm.CorrectRescored
	}
	renderRescoreRow(b, totals)

	fmt.Fprintf(b, "\nA row can be re-scored only when it carries all four answers the policy reads. %d of %d do. The last column counts only the re-scored rows, so it is not comparable with the one before it unless the two middle columns are zero.\n\n",
		totals.Rescored, totals.Rows)
	return b.String()
}

func renderRescoreRow(b *strings.Builder, arm RescoreCount) {
	fmt.Fprintf(b, "| %s | %d | %d | %d | %d | %d | %d | %d | %d |\n",
		arm.Arm, arm.Rows, arm.Rescored, arm.PartialAnswers, arm.VerdictOnly, arm.Refusals, arm.Changed, arm.CorrectRecorded, arm.CorrectRescored)
}
