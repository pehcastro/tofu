package cost

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"tofu/internal/judge/gate"
)

const calibrationFilePath = "bench/cost/calibration/tool_gate@1.json"

//go:embed calibration/tool_gate@1.json
var calibrationFile []byte

type Calibration struct {
	Policy           string         `json:"policy"`
	PolicyVersion    int            `json:"policy_version"`
	Questions        string         `json:"questions"`
	QuestionsVersion int            `json:"questions_version"`
	Build            string         `json:"build"`
	Mode             gate.Mode      `json:"mode"`
	SampleFloor      int            `json:"sample_floor"`
	NFit             int            `json:"n_fit"`
	NVerify          int            `json:"n_verify"`
	Gate             GateThresholds `json:"gate_thresholds"`
	Fit              FittedCut      `json:"fit"`
	Provenance       string         `json:"provenance"`
}

type GateThresholds struct {
	RiskAskAt            float64 `json:"risk_ask_at"`
	RiskDenyAt           float64 `json:"risk_deny_at"`
	UserRequestedRelaxAt float64 `json:"user_requested_relax_at"`
	ApprovalRelaxAt      float64 `json:"approval_relax_at"`
	FromUntrustedBlockAt float64 `json:"from_untrusted_block_at"`
}

func (g GateThresholds) toThresholds() gate.Thresholds {
	return gate.Thresholds{
		RiskAskAt:            g.RiskAskAt,
		RiskDenyAt:           g.RiskDenyAt,
		UserRequestedRelaxAt: g.UserRequestedRelaxAt,
		ApprovalRelaxAt:      g.ApprovalRelaxAt,
		FromUntrustedBlockAt: g.FromUntrustedBlockAt,
	}
}

type FittedCut struct {
	Chosen              float64 `json:"chosen"`
	AgreementFit        float64 `json:"agreement_fit"`
	AgreementVerify     float64 `json:"agreement_verify"`
	AlwaysProceedVerify float64 `json:"always_proceed_verify"`
	BlocksCaughtVerify  int     `json:"blocks_caught_verify"`
	BlocksVerify        int     `json:"blocks_verify"`
	PlateauLow          float64 `json:"plateau_low"`
	PlateauHigh         float64 `json:"plateau_high"`
	Adopted             bool    `json:"adopted"`
}

func GateCalibration() (Calibration, gate.Resolution, error) {
	var calibration Calibration
	if err := json.Unmarshal(calibrationFile, &calibration); err != nil {
		return Calibration{}, gate.Resolution{}, fmt.Errorf("%s: %w", calibrationFilePath, err)
	}
	if calibration.Mode != gate.ModeShadow && calibration.Mode != gate.ModeEnforced {
		return Calibration{}, gate.Resolution{}, fmt.Errorf("%s: mode is %q or %q, found %q", calibrationFilePath, gate.ModeShadow, gate.ModeEnforced, calibration.Mode)
	}
	pol := gate.Rule{
		Name:             calibration.Policy,
		RuleVersion:      calibration.PolicyVersion,
		Questions:        calibration.Questions,
		QuestionsVersion: calibration.QuestionsVersion,
		Mode:             calibration.Mode,
		ModeDeclared:     true,
		SampleFloor:      calibration.SampleFloor,
		File:             calibrationFilePath,
	}
	lock := gate.Lock{
		Rule:             calibration.Policy,
		RuleVersion:      calibration.PolicyVersion,
		Questions:        calibration.Questions,
		QuestionsVersion: calibration.QuestionsVersion,
		Build:            calibration.Build,
		NFit:             calibration.NFit,
		NVerify:          calibration.NVerify,
		Thresholds:       calibration.Gate.toThresholds(),
		PinsThresholds:   true,
		File:             calibrationFilePath,
	}
	current := gate.Current{Build: calibration.Build, QuestionsVersion: calibration.QuestionsVersion, Known: true}
	return calibration, gate.Resolve(pol, gate.LockLookup{Present: true, Lock: lock}, current), nil
}

const (
	fitAnswersPath    = "bench/cost/calibration/fit-2026-09-19.jsonl"
	verifyAnswersPath = "bench/cost/calibration/verify-2026-09-19.jsonl"
)

//go:embed calibration/fit-2026-09-19.jsonl
var fitAnswersFile []byte

//go:embed calibration/verify-2026-09-19.jsonl
var verifyAnswersFile []byte

func CalibrationAnswers() (fit, verify []AnswerRow, err error) {
	fit, err = parseAnswers(fitAnswersFile, fitAnswersPath)
	if err != nil {
		return nil, nil, err
	}
	verify, err = parseAnswers(verifyAnswersFile, verifyAnswersPath)
	if err != nil {
		return nil, nil, err
	}
	return fit, verify, nil
}

const (
	riskScoreCeiling  = 3.0
	riskCutSteps      = 24
	agreementOnePoint = 0.01
)

type CutScore struct {
	Cut         float64
	Cases       int
	Blocks      int
	Correct     int
	CaughtBlock int
	FalseBlock  int
}

func (s CutScore) Agreement() float64 {
	if s.Cases == 0 {
		return 0
	}
	return float64(s.Correct) / float64(s.Cases)
}

func (s CutScore) AlwaysProceedAgreement() float64 {
	if s.Cases == 0 {
		return 0
	}
	return float64(s.Cases-s.Blocks) / float64(s.Cases)
}

type CutFit struct {
	Curve       []CutScore
	Chosen      float64
	OnFit       CutScore
	OnVerify    CutScore
	PlateauLow  float64
	PlateauHigh float64
	PlateauCuts int
	SampleFloor int
	ClearsFloor bool
}

func scoreRows(rows []AnswerRow, cut float64, verdictOf func(AnswerRow) (Verdict, error)) (CutScore, error) {
	score := CutScore{Cut: cut}
	for _, row := range rows {
		verdict, err := verdictOf(row)
		if err != nil {
			return CutScore{}, err
		}
		score.Cases++
		if row.Label == Block {
			score.Blocks++
		}
		if verdict == row.Label {
			score.Correct++
		}
		if verdict != Block {
			continue
		}
		if row.Label == Block {
			score.CaughtBlock++
			continue
		}
		score.FalseBlock++
	}
	return score, nil
}

func ScoreRiskCut(pol gate.Rule, rows []AnswerRow, cut float64) (CutScore, error) {
	pol.Thresholds.RiskAskAt = cut
	return scoreRows(rows, cut, func(row AnswerRow) (Verdict, error) {
		answers, complete := rowAnswers(pol, row)
		if !complete {
			return "", fmt.Errorf("case %s carries a verdict and no answers, so no cut but the one it was decided at can be scored on it", row.Case)
		}
		verdict, _, err := decide(answers, pol)
		return verdict, err
	})
}

const (
	MissedBlockCost = 10.0
	FalseBlockCost  = 1.0
)

func (s CutScore) WeightedCost(missCost, falseCost float64) float64 {
	missed := s.Blocks - s.CaughtBlock
	return missCost*float64(missed) + falseCost*float64(s.FalseBlock)
}

type WeightedFit struct {
	MissCost  float64
	FalseCost float64
	Curve     []CutScore
	Chosen    float64
	OnFit     CutScore
	OnVerify  CutScore
}

func FitRiskCutWeighted(pol gate.Rule, fit, verify []AnswerRow, missCost, falseCost float64) (WeightedFit, error) {
	result := WeightedFit{MissCost: missCost, FalseCost: falseCost}
	curve, err := riskCurve(pol, fit)
	if err != nil {
		return WeightedFit{}, err
	}
	result.Curve = curve
	best := curve[0]
	bestCost := best.WeightedCost(missCost, falseCost)
	for _, score := range curve[1:] {
		cost := score.WeightedCost(missCost, falseCost)
		tie := cost == bestCost && score.FalseBlock < best.FalseBlock
		if cost < bestCost || tie {
			best, bestCost = score, cost
		}
	}
	result.Chosen, result.OnFit = best.Cut, best
	onVerify, err := ScoreRiskCut(pol, verify, best.Cut)
	if err != nil {
		return WeightedFit{}, err
	}
	result.OnVerify = onVerify
	return result, nil
}

type BudgetFit struct {
	AllowedFalse int
	Chosen       float64
	OnFit        CutScore
	OnVerify     CutScore
	Feasible     bool
}

func FitUnderFalseBlockBudget(pol gate.Rule, fit, verify []AnswerRow, budget float64) (BudgetFit, error) {
	curve, err := riskCurve(pol, fit)
	if err != nil {
		return BudgetFit{}, err
	}
	proceeds := curve[0].Cases - curve[0].Blocks
	result := BudgetFit{AllowedFalse: int(budget * float64(proceeds))}
	for _, score := range curve {
		if score.FalseBlock > result.AllowedFalse {
			continue
		}
		if result.Feasible && score.CaughtBlock <= result.OnFit.CaughtBlock {
			continue
		}
		result.Feasible, result.Chosen, result.OnFit = true, score.Cut, score
	}
	if !result.Feasible {
		return result, nil
	}
	onVerify, err := ScoreRiskCut(pol, verify, result.Chosen)
	if err != nil {
		return BudgetFit{}, err
	}
	result.OnVerify = onVerify
	return result, nil
}

func riskCurve(pol gate.Rule, rows []AnswerRow) ([]CutScore, error) {
	var curve []CutScore
	for step := 0; step <= riskCutSteps; step++ {
		score, err := ScoreRiskCut(pol, rows, riskScoreCeiling*float64(step)/riskCutSteps)
		if err != nil {
			return nil, err
		}
		curve = append(curve, score)
	}
	return curve, nil
}

func FitRiskCut(pol gate.Rule, fit, verify []AnswerRow, floor int) (CutFit, error) {
	result := CutFit{SampleFloor: floor, ClearsFloor: len(fit) >= floor && len(verify) >= floor}
	curve, err := riskCurve(pol, fit)
	if err != nil {
		return CutFit{}, err
	}
	result.Curve = curve
	best := result.Curve[0]
	for _, score := range result.Curve[1:] {
		ahead := score.Correct > best.Correct
		levelAndCleaner := score.Correct == best.Correct && score.FalseBlock < best.FalseBlock
		if ahead || levelAndCleaner {
			best = score
		}
	}
	result.Chosen, result.OnFit = best.Cut, best
	result.PlateauLow, result.PlateauHigh = best.Cut, best.Cut
	for _, score := range result.Curve {
		if best.Agreement()-score.Agreement() > agreementOnePoint {
			continue
		}
		result.PlateauCuts++
		result.PlateauLow = min(result.PlateauLow, score.Cut)
		result.PlateauHigh = max(result.PlateauHigh, score.Cut)
	}
	onVerify, err := ScoreRiskCut(pol, verify, best.Cut)
	if err != nil {
		return CutFit{}, err
	}
	result.OnVerify = onVerify
	return result, nil
}
