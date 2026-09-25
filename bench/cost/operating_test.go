package cost

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"

	benchapi "tofu/bench/api"
	"tofu/bench/corpus"
	"tofu/internal/judge/gate"
)

const barWidth = 30

func TestLiveCalibrationPassOverBothHalves(t *testing.T) {
	key := liveKey(t, "TOFU_LIVE_CALIBRATION")
	battery, _, err := benchapi.GateBattery()
	if err != nil {
		t.Fatalf("GateBattery: %v", err)
	}
	pol, _ := shippedGate(t)
	fitRecords, verifyRecords := halves(t)
	day := time.Now().Format("2006-01-02")
	for _, half := range []struct {
		name    string
		records []corpus.Record
	}{{"fit", fitRecords}, {"verify", verifyRecords}} {
		arm, err := runJev(context.Background(), key, half.records, battery, pol)
		if err != nil {
			t.Fatalf("the jev arm stopped on the %s half: %v", half.name, err)
		}
		if arm.Stopped != "" {
			t.Fatalf("the %s half is incomplete: %s", half.name, arm.Stopped)
		}
		source := fmt.Sprintf("the %s half of bench/corpus/gate/split.json, asked live on %s through %s, every question answer kept", half.name, day, pol.File)
		path := filepath.Join("calibration", fmt.Sprintf("%s-%s.jsonl", half.name, day))
		if err := writeAnswers(path, answerRows([]ArmResult{arm}, pol, source)); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}
		t.Logf("%s: %d cases, %d correct at the shipped thresholds, %d input tokens, $%.6f, p50 %.0f ms, model %s, wrote %s",
			half.name, len(arm.Cases), arm.CorrectCount, arm.TotalInputTokens, arm.Total.Money, medianLatency(arm), modelOf(arm), path)
	}
}

func calibrationHalves(t *testing.T) (fit, verify []AnswerRow) {
	t.Helper()
	fit, verify, err := CalibrationAnswers()
	if err != nil {
		t.Fatalf("reading the calibration answers: %v", err)
	}
	fitRecords, verifyRecords := halves(t)
	if len(fit) != len(fitRecords) || len(verify) != len(verifyRecords) {
		t.Fatalf("the calibration holds %d fit and %d verify rows, the split names %d and %d",
			len(fit), len(verify), len(fitRecords), len(verifyRecords))
	}
	return fit, verify
}

func TestTheCutIsFittedOnOneHalfAndScoredOnTheOther(t *testing.T) {
	pol, _ := shippedGate(t)
	fit, verify := calibrationHalves(t)
	result, err := FitRiskCut(pol, fit, verify, pol.SampleFloor)
	if err != nil {
		t.Fatalf("fitting the risk cut: %v", err)
	}
	t.Logf("fit %d cases, verify %d cases, floor %d, clears %v", len(fit), len(verify), result.SampleFloor, result.ClearsFloor)
	for _, score := range result.Curve {
		bar := int(float64(barWidth)*score.Agreement() + 0.5)
		t.Logf("cut %.3f %5.1f%% %-*s correct %3d/%3d  caught %d/%d  false %2d",
			score.Cut, 100*score.Agreement(), barWidth, strings.Repeat("#", bar),
			score.Correct, score.Cases, score.CaughtBlock, score.Blocks, score.FalseBlock)
	}
	t.Logf("chosen cut %.3f, fit %.1f%%, verify %.1f%%, plateau %.3f to %.3f over %d of %d cuts",
		result.Chosen, 100*result.OnFit.Agreement(), 100*result.OnVerify.Agreement(),
		result.PlateauLow, result.PlateauHigh, result.PlateauCuts, len(result.Curve))
	for _, score := range []struct {
		half  string
		score CutScore
	}{{"fit", result.OnFit}, {"verify", result.OnVerify}} {
		t.Logf("%s at the chosen cut: %d/%d correct, %d of %d blocks caught, %d false blocks, always-proceed scores %.1f%%",
			score.half, score.score.Correct, score.score.Cases, score.score.CaughtBlock, score.score.Blocks,
			score.score.FalseBlock, 100*score.score.AlwaysProceedAgreement())
	}
	shipped, err := ScoreRiskCut(pol, verify, pol.Thresholds.RiskAskAt)
	if err != nil {
		t.Fatalf("scoring the shipped cut: %v", err)
	}
	t.Logf("verify at the shipped cut %.3f: %d/%d correct, %d of %d blocks caught, %d false blocks",
		shipped.Cut, shipped.Correct, shipped.Cases, shipped.CaughtBlock, shipped.Blocks, shipped.FalseBlock)
	if result.OnVerify.Cases == 0 {
		t.Fatal("the chosen cut was scored on no verify case at all")
	}
}

func TestTheGateIsScoredAgainstAlwaysProceedOnBothHalves(t *testing.T) {
	pol, _ := shippedGate(t)
	fit, verify := calibrationHalves(t)
	for _, half := range []struct {
		name string
		rows []AnswerRow
	}{{"fit", fit}, {"verify", verify}} {
		five, err := ScoreRiskCut(pol, half.rows, pol.Thresholds.RiskAskAt)
		if err != nil {
			t.Fatalf("gate.Decide over the %s half: %v", half.name, err)
		}
		t.Logf("%s, gate.Decide from library/general/rules/tool_gate@1.yaml, risk_ask_at %.2f user_requested_relax_at %.2f: %d/%d correct, %d of %d blocks caught, %d false blocks",
			half.name, pol.Thresholds.RiskAskAt, pol.Thresholds.UserRequestedRelaxAt,
			five.Correct, five.Cases, five.CaughtBlock, five.Blocks, five.FalseBlock)
		t.Logf("%s, always-proceed, no rule and no cut: %d/%d correct, 0 of %d blocks caught, 0 false blocks",
			half.name, five.Cases-five.Blocks, five.Cases, five.Blocks)
	}
}

func separationDirection(p float64, gateWins, otherWins int) string {
	if p >= separationAlpha {
		return "indistinguishable from always-proceed"
	}
	if otherWins > gateWins {
		return "separated, the gate is worse"
	}
	return "separated, the gate is better"
}

func TestTheFitUnderAFalseBlockBudgetIsScoredOnTheVerifyHalf(t *testing.T) {
	pol, _ := shippedGate(t)
	fit, verify := calibrationHalves(t)
	for _, budget := range []float64{0.00, 0.01, 0.02, 0.05, 0.10, 0.20, 0.40} {
		result, err := FitUnderFalseBlockBudget(pol, fit, verify, budget)
		if err != nil {
			t.Fatalf("fitting under a budget of %.2f: %v", budget, err)
		}
		if !result.Feasible {
			t.Logf("budget %4.0f%% allows %d false blocks: no cut is feasible", 100*budget, result.AllowedFalse)
			continue
		}
		p := twoSidedSignP(result.OnVerify.CaughtBlock, result.OnVerify.FalseBlock)
		direction := separationDirection(p, result.OnVerify.CaughtBlock, result.OnVerify.FalseBlock)
		t.Logf("budget %4.0f%% allows %2d false: cut %.3f, fit caught %d/%d false %2d, verify caught %d/%d false %2d, verify %d/%d against always-proceed %d/%d, sign p %.4f, %s",
			100*budget, result.AllowedFalse, result.Chosen,
			result.OnFit.CaughtBlock, result.OnFit.Blocks, result.OnFit.FalseBlock,
			result.OnVerify.CaughtBlock, result.OnVerify.Blocks, result.OnVerify.FalseBlock,
			result.OnVerify.Correct, result.OnVerify.Cases,
			result.OnVerify.Cases-result.OnVerify.Blocks, result.OnVerify.Cases, p, direction)
	}
}

func TestAHigherRiskCutNeverBlocksMoreThanALowerOne(t *testing.T) {
	pol, _ := shippedGate(t)
	rows := []AnswerRow{}
	for risk := 0.0; risk <= riskScoreCeiling; risk += 0.25 {
		untrusted := 0.0
		rows = append(rows, AnswerRow{
			Case: "grid", Arm: "grid", Label: Proceed, AnswersKnown: true,
			Risk: &risk, FromUntrusted: &untrusted, Approval: 0.5, UserRequested: 0.5,
		})
	}
	previous := len(rows) + 1
	for step := 0; step <= riskCutSteps; step++ {
		score, err := ScoreRiskCut(pol, rows, riskScoreCeiling*float64(step)/riskCutSteps)
		if err != nil {
			t.Fatalf("scoring: %v", err)
		}
		blocked := score.CaughtBlock + score.FalseBlock
		if blocked > previous {
			t.Fatalf("cut %.3f blocks %d and the cut below it blocked %d", score.Cut, blocked, previous)
		}
		previous = blocked
	}
}

func TestTheCalibrationCarriesTheFiveThresholdsTheGateReadsAndStillStaysShadow(t *testing.T) {
	calibration, resolution, err := GateCalibration()
	if err != nil {
		t.Fatalf("GateCalibration: %v", err)
	}
	shipped, _ := shippedGate(t)
	if calibration.Gate.toThresholds() != shipped.Thresholds {
		t.Fatalf("the calibration pins %s and library/general/rules/tool_gate@1.yaml ships %s", calibration.Gate.toThresholds(), shipped.Thresholds)
	}
	if calibration.NFit != 89 || calibration.NVerify != 89 {
		t.Fatalf("n_fit %d and n_verify %d, and the split holds 89 on each side", calibration.NFit, calibration.NVerify)
	}
	if calibration.Mode != gate.ModeEnforced {
		t.Fatalf("the file declares %s, and the arms did decide through it", calibration.Mode)
	}
	if resolution.Mode != gate.ModeShadow || resolution.Pinned {
		t.Fatalf("mode %s pinned %v on %d fitted cases against a floor of %d", resolution.Mode, resolution.Pinned, calibration.NFit, calibration.SampleFloor)
	}
	if !strings.Contains(resolution.Reason, "below the floor") {
		t.Fatalf("reason = %q, want the sample floor named", resolution.Reason)
	}
	if calibration.Fit.Adopted {
		t.Fatal("the calibration says the fitted cut was adopted, and BOJI-122 measured it without adopting it")
	}
	t.Logf("resolution: %s, %s", resolution.Mode, resolution.Reason)
}

func TestTheCalibrationFileRecordsTheFitItClaims(t *testing.T) {
	pol, _ := shippedGate(t)
	fit, verify := calibrationHalves(t)
	result, err := FitRiskCut(pol, fit, verify, pol.SampleFloor)
	if err != nil {
		t.Fatalf("fitting the risk cut: %v", err)
	}
	calibration, _, err := GateCalibration()
	if err != nil {
		t.Fatalf("GateCalibration: %v", err)
	}
	recorded := calibration.Fit
	for _, check := range []struct {
		field      string
		recorded   float64
		recomputed float64
	}{
		{"chosen", recorded.Chosen, result.Chosen},
		{"agreement_fit", recorded.AgreementFit, result.OnFit.Agreement()},
		{"agreement_verify", recorded.AgreementVerify, result.OnVerify.Agreement()},
		{"always_proceed_verify", recorded.AlwaysProceedVerify, result.OnVerify.AlwaysProceedAgreement()},
		{"plateau_low", recorded.PlateauLow, result.PlateauLow},
		{"plateau_high", recorded.PlateauHigh, result.PlateauHigh},
	} {
		if math.Abs(check.recorded-check.recomputed) > 0.0001 {
			t.Errorf("%s: the file says %.4f and the fit computes %.4f", check.field, check.recorded, check.recomputed)
		}
	}
	if recorded.BlocksCaughtVerify != result.OnVerify.CaughtBlock || recorded.BlocksVerify != result.OnVerify.Blocks {
		t.Errorf("the file says %d of %d blocks caught and the fit computes %d of %d",
			recorded.BlocksCaughtVerify, recorded.BlocksVerify, result.OnVerify.CaughtBlock, result.OnVerify.Blocks)
	}
}

func TestTheWeightedFitBeatsNeverBlockingOnItsOwnCost(t *testing.T) {
	pol, _ := shippedGate(t)
	fit, _ := calibrationHalves(t)
	result, err := FitRiskCutWeighted(pol, fit, fit, MissedBlockCost, FalseBlockCost)
	if err != nil {
		t.Fatalf("fitting the weighted cut: %v", err)
	}
	neverBlock := result.Curve[len(result.Curve)-1]
	chosenCost := result.OnFit.WeightedCost(MissedBlockCost, FalseBlockCost)
	neverBlockCost := neverBlock.WeightedCost(MissedBlockCost, FalseBlockCost)
	if chosenCost > neverBlockCost {
		t.Fatalf("chosen cut %.3f costs %.1f at weighting %.0f:%.0f, worse than never blocking at %.1f",
			result.Chosen, chosenCost, MissedBlockCost, FalseBlockCost, neverBlockCost)
	}
	t.Logf("weighting %.0f:%.0f (miss:false), chosen cut %.3f: fit costs %.1f against never-blocking's %.1f",
		MissedBlockCost, FalseBlockCost, result.Chosen, chosenCost, neverBlockCost)
}

func TestTheWeightedFitIsScoredOnTheVerifyHalfAgainstAlwaysProceed(t *testing.T) {
	pol, _ := shippedGate(t)
	fit, verify := calibrationHalves(t)
	for _, missCost := range []float64{1, 3, 10, 30, 100} {
		result, err := FitRiskCutWeighted(pol, fit, verify, missCost, FalseBlockCost)
		if err != nil {
			t.Fatalf("fitting at %.0f:1: %v", missCost, err)
		}
		p := twoSidedSignP(result.OnVerify.CaughtBlock, result.OnVerify.FalseBlock)
		direction := separationDirection(p, result.OnVerify.CaughtBlock, result.OnVerify.FalseBlock)
		t.Logf("weighting %5.0f:1  cut %.3f  fit caught %d/%d false %2d  verify caught %d/%d false %2d  correct %d/%d  sign p %.4f  %s",
			missCost, result.Chosen,
			result.OnFit.CaughtBlock, result.OnFit.Blocks, result.OnFit.FalseBlock,
			result.OnVerify.CaughtBlock, result.OnVerify.Blocks, result.OnVerify.FalseBlock,
			result.OnVerify.Correct, result.OnVerify.Cases, p, direction)
	}
}

func TestTheFittedCutDoesNotSeparateFromAlwaysProceed(t *testing.T) {
	pol, _ := shippedGate(t)
	fit, verify := calibrationHalves(t)
	result, err := FitRiskCut(pol, fit, verify, pol.SampleFloor)
	if err != nil {
		t.Fatalf("fitting the risk cut: %v", err)
	}
	cutWins, constantWins := result.OnVerify.CaughtBlock, result.OnVerify.FalseBlock
	p := twoSidedSignP(cutWins, constantWins)
	t.Logf("verify at cut %.3f: the cut is right and always-proceed wrong on %d cases, the reverse on %d, sign test p %.4f",
		result.Chosen, cutWins, constantWins, p)
	if p < separationAlpha {
		t.Fatalf("the fitted cut separates from the constant at p %.4f, and BOJI-122 reported that it does not", p)
	}
}
