package cost

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	benchapi "boji/bench/api"
	"boji/internal/judge/jev"
)

type CaseResult struct {
	Case          string
	ModelID       string
	InputTokens   int
	OutputTokens  int
	Cost          float64
	LatencyMS     float64
	UserRequested float64
	Approval      float64
	Verdict       Verdict
	Label         Verdict
	Correct       bool
	Refusal       string
}

type ArmResult struct {
	Arm               string
	Cases             []CaseResult
	TotalCost         float64
	TotalInputTokens  int
	CorrectCount      int
	CostPerCorrect    float64
	HasCostPerCorrect bool
}

type Disagreement struct {
	Arm           string
	Case          string
	State         string
	UserRequested float64
	Approval      float64
	Answer        Verdict
	Label         Verdict
	LabelSource   string
	Refusal       string
}

type Result struct {
	GeneratedAt            time.Time
	Arms                   []ArmResult
	Disagreements          []Disagreement
	RegexMatchesLabel      []string
	ContextTokensAvoided   int
	ContextDollarsAvoided  float64
	FrontierRatePerToken   float64
	FrontierRateFromCost   float64
	FrontierRateFromTokens int
	TotalSpend             float64
}

func Run(ctx context.Context, key string) (Result, error) {
	cases, err := benchapi.GateCases()
	if err != nil {
		return Result{}, err
	}
	battery, _, err := benchapi.GateBattery()
	if err != nil {
		return Result{}, err
	}

	jevArm, err := runJev(ctx, key, cases, battery)
	if err != nil {
		return Result{}, err
	}
	opusArm, err := runModel(ctx, key, ModelOpus, cases, battery)
	if err != nil {
		return Result{}, err
	}
	fableArm, err := runModel(ctx, key, ModelFable, cases, battery)
	if err != nil {
		return Result{}, err
	}
	regexArm, err := runRegex(cases)
	if err != nil {
		return Result{}, err
	}

	states := make(map[string]string, len(cases))
	for _, gateCase := range cases {
		raw, err := json.Marshal(gateCase.State)
		if err != nil {
			return Result{}, fmt.Errorf("bench/cost: encoding %s for the report: %w", gateCase.Name, err)
		}
		states[gateCase.Name] = string(raw)
	}

	result := Result{
		GeneratedAt: time.Now(),
		Arms:        []ArmResult{jevArm, regexArm, opusArm, fableArm},
	}
	for _, arm := range result.Arms {
		result.TotalSpend += arm.TotalCost
		for _, c := range arm.Cases {
			if !c.Correct {
				result.Disagreements = append(result.Disagreements, Disagreement{
					Arm: arm.Arm, Case: c.Case, State: states[c.Case],
					UserRequested: c.UserRequested, Approval: c.Approval,
					Answer: c.Verdict, Label: c.Label, LabelSource: labelSource, Refusal: c.Refusal,
				})
			}
			if arm.Arm == "regex" && c.Correct {
				result.RegexMatchesLabel = append(result.RegexMatchesLabel, c.Case)
			}
		}
	}

	for _, c := range jevArm.Cases {
		result.ContextTokensAvoided += c.InputTokens
	}
	result.FrontierRateFromCost = opusArm.TotalCost + fableArm.TotalCost
	result.FrontierRateFromTokens = opusArm.TotalInputTokens + fableArm.TotalInputTokens
	if result.FrontierRateFromTokens > 0 {
		result.FrontierRatePerToken = result.FrontierRateFromCost / float64(result.FrontierRateFromTokens)
	}
	result.ContextDollarsAvoided = float64(result.ContextTokensAvoided) * result.FrontierRatePerToken

	return result, nil
}

func buildArm(name string, cases []benchapi.GateCase, ask func(benchapi.GateCase) (CaseResult, error)) (ArmResult, error) {
	arm := ArmResult{Arm: name}
	for _, gateCase := range cases {
		result, err := ask(gateCase)
		if err != nil {
			return ArmResult{}, fmt.Errorf("%s arm, case %s: %w", name, gateCase.Name, err)
		}
		arm.Cases = append(arm.Cases, result)
		arm.TotalCost += result.Cost
		arm.TotalInputTokens += result.InputTokens
		if result.Correct {
			arm.CorrectCount++
		}
	}
	if arm.CorrectCount > 0 {
		arm.CostPerCorrect = arm.TotalCost / float64(arm.CorrectCount)
		arm.HasCostPerCorrect = true
	}
	return arm, nil
}

func msSince(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}

func runJev(ctx context.Context, key string, cases []benchapi.GateCase, battery []jev.Question) (ArmResult, error) {
	wire, err := benchapi.NewWire(key)
	if err != nil {
		return ArmResult{}, err
	}
	return buildArm("jev", cases, func(gateCase benchapi.GateCase) (CaseResult, error) {
		call := benchapi.Ask(ctx, wire, jev.Request{State: gateCase.State, Questions: battery})
		if call.Err != nil {
			return CaseResult{}, call.Err
		}
		userRequested := call.Response.Answers["user_requested"].Noul
		approval := call.Response.Answers["approval"].Noul
		verdict := Decide(userRequested, approval)
		label := Labels[gateCase.Name].Verdict
		return CaseResult{
			Case: gateCase.Name, ModelID: call.Response.Build,
			InputTokens: call.Response.Usage.InputTokens, OutputTokens: call.Response.Usage.OutputTokens,
			Cost: call.Response.Usage.Cost, LatencyMS: msSince(call.Raw.Latency),
			UserRequested: userRequested, Approval: approval, Verdict: verdict, Label: label, Correct: verdict == label,
		}, nil
	})
}

func runModel(ctx context.Context, key, model string, cases []benchapi.GateCase, battery []jev.Question) (ArmResult, error) {
	wire, err := NewModelWire(key, model)
	if err != nil {
		return ArmResult{}, err
	}
	name := "opus"
	if model == ModelFable {
		name = "fable"
	}
	return buildArm(name, cases, func(gateCase benchapi.GateCase) (CaseResult, error) {
		modelResult, err := wire.Ask(ctx, gateCase.State, battery)
		if err != nil {
			return CaseResult{}, err
		}
		label := Labels[gateCase.Name].Verdict
		if modelResult.Refused {
			return CaseResult{
				Case: gateCase.Name, ModelID: modelResult.Model,
				InputTokens: modelResult.InputTokens, OutputTokens: modelResult.OutputTokens,
				Cost: modelResult.Cost, LatencyMS: msSince(modelResult.Latency),
				Verdict: Refused, Label: label, Correct: false, Refusal: modelResult.Refusal,
			}, nil
		}
		verdict := Decide(modelResult.Answer.UserRequested, modelResult.Answer.Approval)
		return CaseResult{
			Case: gateCase.Name, ModelID: modelResult.Model,
			InputTokens: modelResult.InputTokens, OutputTokens: modelResult.OutputTokens,
			Cost: modelResult.Cost, LatencyMS: msSince(modelResult.Latency),
			UserRequested: modelResult.Answer.UserRequested, Approval: modelResult.Answer.Approval,
			Verdict: verdict, Label: label, Correct: verdict == label,
		}, nil
	})
}

func runRegex(cases []benchapi.GateCase) (ArmResult, error) {
	return buildArm("regex", cases, func(gateCase benchapi.GateCase) (CaseResult, error) {
		start := time.Now()
		verdict := RegexDecide(extractCommand(gateCase.State), extractUserMessage(gateCase.State))
		label := Labels[gateCase.Name].Verdict
		return CaseResult{
			Case: gateCase.Name, ModelID: "regexp", LatencyMS: msSince(time.Since(start)),
			Verdict: verdict, Label: label, Correct: verdict == label,
		}, nil
	})
}
