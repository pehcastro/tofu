package cost

import (
	"fmt"
	"strings"
)

func RenderSweep(result SweepResult) string {
	b := &strings.Builder{}
	fmt.Fprintf(b, "Model calls made by the sweep: %d. The rows come from %s and every one of them is marked recorded, so the counter has nothing to count.\n\n", result.ModelCalls, recordedAnswersPath)
	fmt.Fprintf(b, "The published point is user_requested_override_at %.2f, approval_block_at %.2f. The sweep moves approval_block_at over %d steps and leaves the override where it was.\n\n",
		result.Published.UserRequestedOverrideAt, result.Published.ApprovalBlockAt, result.Steps)
	for _, curve := range result.Arms {
		renderCurve(b, curve)
	}
	return b.String()
}

func renderCurve(b *strings.Builder, curve ArmCurve) {
	fmt.Fprintf(b, "### %s\n\n", curve.Arm)
	fmt.Fprintf(b, "%d cases, %d labelled block and %d labelled proceed. %d carry the recorded answers, %d carry the recorded verdict alone, %d are refusals that no threshold moves. A constant that always proceeds scores %d on this set.\n\n",
		curve.Cases, curve.Blocks, curve.AlwaysProceed, curve.AnswersKnown, curve.Cases-curve.AnswersKnown-curve.Refusals, curve.Refusals, curve.AlwaysProceed)
	b.WriteString("| approval blocks at | correct | agreement | false blocks | blocks caught | undetermined |\n|---|---|---|---|---|---|\n")
	for _, point := range curve.Points {
		fmt.Fprintf(b, "| %.2f | %s | %s | %s | %s | %d |\n",
			point.ApprovalBlockAt,
			band(point.CorrectLow, point.CorrectHigh),
			agreementBand(point, curve.Cases),
			band(point.FalseBlockLow, point.FalseBlockHigh),
			band(point.CaughtLow, point.CaughtHigh),
			point.Undetermined)
	}
	fmt.Fprintf(b, "\nAt the published %.2f: %s correct, %s false blocks. At its own best %.2f: %s correct, %s false blocks. Against the constant's %d, %s.\n\n",
		curve.AtPublished.ApprovalBlockAt, band(curve.AtPublished.CorrectLow, curve.AtPublished.CorrectHigh), band(curve.AtPublished.FalseBlockLow, curve.AtPublished.FalseBlockHigh),
		curve.Best.ApprovalBlockAt, band(curve.Best.CorrectLow, curve.Best.CorrectHigh), band(curve.Best.FalseBlockLow, curve.Best.FalseBlockHigh),
		curve.AlwaysProceed, verdictAgainstTheConstant(curve))
}

func verdictAgainstTheConstant(curve ArmCurve) string {
	switch {
	case curve.Best.CorrectLow > curve.AlwaysProceed:
		return "the arm wins at its best point on every reading of the undetermined cases"
	case curve.Best.CorrectHigh < curve.AlwaysProceed:
		return "the arm loses at its best point on every reading of the undetermined cases"
	case curve.Best.CorrectLow == curve.AlwaysProceed && curve.Best.CorrectHigh == curve.AlwaysProceed:
		return "the arm ties at its best point"
	default:
		return "the arm ties or wins at its best point, and which one it is depends on the undetermined cases"
	}
}

func band(low, high int) string {
	if low == high {
		return fmt.Sprintf("%d", low)
	}
	return fmt.Sprintf("%d to %d", low, high)
}

func agreementBand(point CurvePoint, cases int) string {
	low := 100 * float64(point.CorrectLow) / float64(cases)
	high := 100 * float64(point.CorrectHigh) / float64(cases)
	if point.CorrectLow == point.CorrectHigh {
		return fmt.Sprintf("%.1f%%", low)
	}
	return fmt.Sprintf("%.1f%% to %.1f%%", low, high)
}
