package cost

import (
	"fmt"
	"strings"

	"tofu/internal/judge/gate"
	"tofu/internal/konst"
)

const deterministicArmSource = "recomputed from the held-out half: these two arms read the state and ask no question, so no rule decides for them and nothing about them can change"

func RenderRescore(result RescoreResult) string {
	b := &strings.Builder{}
	p := result.Rule
	fmt.Fprintf(b, "Every number below is decided by `gate.Decide` reading `%s`, the five threshold rule the live gate runs. Revisions of this report before 2026-09-19 scored with `DecideByApproval`, a two threshold rule nothing under `cmd/` or `internal/` calls, and its figures are not comparable with these.\n\n", p.File)
	calibrated := 0
	for _, arm := range result.Arms {
		if arm.Arm == CalibrationArm {
			calibrated = arm.Rows
		}
	}
	fmt.Fprintf(b, "The re-score makes no model call of its own. %d of its rows carry answers that came from a live call when they were first recorded, %d of those from the calibration pass of BOJI-122 in `%s` and `%s`, which is the only source that kept all four answers. The others come from `%s`, and the regex and always-proceed arms were %s\n\n",
		result.ModelCalls, calibrated, fitAnswersPath, verifyAnswersPath, recordedAnswersPath, deterministicArmSource)
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

	fmt.Fprintf(b, "\nA row can be re-scored only when it carries all four answers the rule reads. %d of %d do. The last column counts only the re-scored rows, so it is not comparable with the one before it unless the two middle columns are zero.\n\n",
		totals.Rescored, totals.Rows)
	renderDeadBand(b, p, result.Rows)
	return b.String()
}

func renderDeadBand(b *strings.Builder, pol gate.Rule, rows []AnswerRow) {
	carryAnswers := 0
	inBand := map[string]bool{}
	table := &strings.Builder{}
	for _, row := range rows {
		if !row.AnswersKnown {
			continue
		}
		carryAnswers++
		for _, cut := range ruleCuts(pol, row) {
			distance := cut.value - cut.threshold
			if distance > konst.ThresholdDeadBand || distance < -konst.ThresholdDeadBand {
				continue
			}
			inBand[row.Arm+row.Case] = true
			fmt.Fprintf(table, "| %s | %s | %s | %.2f | %+.2f | %s |\n", row.Arm, row.Case, cut.question, cut.value, distance, row.Verdict)
		}
	}
	fmt.Fprintf(b, "## Rows whose answer sat on the cut\n\n%d of the %d rows that carry answers sit inside the dead band of %.2f, measured from this instrument's own rerun spread and read from `konst.ThresholdDeadBand`. The cuts are the ones `gate.Decide` compares against, read from `%s`: risk %.2f and %.2f, user_requested %.2f, approval %.2f, from_untrusted %.2f. A row this close to a cut answers differently between two runs of the same question on the same state, so its verdict is a coin flip and the counts above, which include it, move by that much for no reason anyone changed.\n\n",
		len(inBand), carryAnswers, konst.ThresholdDeadBand, pol.File,
		pol.Thresholds.RiskAskAt, pol.Thresholds.RiskDenyAt,
		pol.Thresholds.UserRequestedRelaxAt, pol.Thresholds.ApprovalRelaxAt, pol.Thresholds.FromUntrustedBlockAt)
	b.WriteString("| Arm | Case | Question | Answer | Distance from the cut | Verdict recorded |\n|---|---|---|---|---|---|\n")
	b.WriteString(table.String())
	b.WriteString("\n")
}

type questionCut struct {
	question  string
	value     float64
	threshold float64
}

func ruleCuts(pol gate.Rule, row AnswerRow) []questionCut {
	cuts := []questionCut{
		{pol.UserRequestedQuestion, row.UserRequested, pol.Thresholds.UserRequestedRelaxAt},
		{pol.ApprovalQuestion, row.Approval, pol.Thresholds.ApprovalRelaxAt},
	}
	if row.Risk != nil {
		cuts = append(cuts,
			questionCut{pol.RiskQuestion, *row.Risk, pol.Thresholds.RiskAskAt},
			questionCut{pol.RiskQuestion, *row.Risk, pol.Thresholds.RiskDenyAt})
	}
	if row.FromUntrusted != nil {
		cuts = append(cuts, questionCut{pol.FromUntrustedQuestion, *row.FromUntrusted, pol.Thresholds.FromUntrustedBlockAt})
	}
	return cuts
}

func renderRescoreRow(b *strings.Builder, arm RescoreCount) {
	fmt.Fprintf(b, "| %s | %d | %d | %d | %d | %d | %d | %d | %d |\n",
		arm.Arm, arm.Rows, arm.Rescored, arm.PartialAnswers, arm.VerdictOnly, arm.Refusals, arm.Changed, arm.CorrectRecorded, arm.CorrectRescored)
}
