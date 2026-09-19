package cost

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	benchapi "boji/bench/api"
	"boji/internal/judge/jev"
	"boji/internal/judge/ledger"
)

type CaseResult struct {
	Case          string
	ModelID       string
	InputTokens   int
	OutputTokens  int
	Money         ledger.Money
	List          ledger.ListPrice
	LatencyMS     float64
	UserRequested float64
	Approval      float64
	Verdict       Verdict
	Label         Verdict
	Correct       bool
	Refusal       string
}

type ArmResult struct {
	Arm              string
	Unit             ledger.Unit
	Cases            []CaseResult
	Total            ledger.Spend
	TotalInputTokens int
	CorrectCount     int
	MoneyPerCorrect  ledger.Money
	ListPerCorrect   ledger.ListPrice
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

type Headline struct {
	FrontierArm              string
	FrontierUnit             ledger.Unit
	JevMoneyPerCorrect       ledger.Money
	FrontierMoneyPerCorrect  ledger.Money
	JevTokensPerCorrect      int
	FrontierTokensPerCorrect int
	Ratio                    float64
	HasRatio                 bool
}

type Result struct {
	GeneratedAt            time.Time
	Arms                   []ArmResult
	Disagreements          []Disagreement
	RegexMatchesLabel      []string
	ContextTokensAvoided   int
	ContextDollarsAvoided  ledger.Money
	FrontierUnit           ledger.Unit
	FrontierRatePerToken   float64
	FrontierRateFromMoney  ledger.Money
	FrontierRateFromTokens int
	Total                  ledger.Spend
	Headline               Headline
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
		result.Total = result.Total.Plus(arm.Total)
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

	result.ContextTokensAvoided = jevArm.TotalInputTokens
	result.FrontierUnit = opusArm.Unit
	result.FrontierRateFromMoney = opusArm.Total.Money + fableArm.Total.Money
	result.FrontierRateFromTokens = opusArm.TotalInputTokens + fableArm.TotalInputTokens
	if result.FrontierUnit == ledger.UnitMoney && result.FrontierRateFromTokens > 0 {
		result.FrontierRatePerToken = float64(result.FrontierRateFromMoney) / float64(result.FrontierRateFromTokens)
		result.ContextDollarsAvoided = ledger.Money(float64(result.ContextTokensAvoided) * result.FrontierRatePerToken)
	}
	result.Headline = headline(jevArm, opusArm)

	return result, nil
}

func headline(jevArm, frontierArm ArmResult) Headline {
	line := Headline{
		FrontierArm:        frontierArm.Arm,
		FrontierUnit:       frontierArm.Unit,
		JevMoneyPerCorrect: jevArm.MoneyPerCorrect,
	}
	if jevArm.CorrectCount > 0 {
		line.JevTokensPerCorrect = jevArm.TotalInputTokens / jevArm.CorrectCount
	}
	if frontierArm.CorrectCount > 0 {
		line.FrontierTokensPerCorrect = frontierArm.TotalInputTokens / frontierArm.CorrectCount
		line.FrontierMoneyPerCorrect = frontierArm.MoneyPerCorrect
	}
	if jevArm.Unit == ledger.UnitMoney && frontierArm.Unit == ledger.UnitMoney && jevArm.MoneyPerCorrect > 0 {
		line.Ratio = float64(frontierArm.MoneyPerCorrect) / float64(jevArm.MoneyPerCorrect)
		line.HasRatio = true
	}
	return line
}

func buildArm(name string, unit ledger.Unit, cases []benchapi.GateCase, ask func(benchapi.GateCase) (CaseResult, error)) (ArmResult, error) {
	arm := ArmResult{Arm: name, Unit: unit}
	for _, gateCase := range cases {
		result, err := ask(gateCase)
		if err != nil {
			return ArmResult{}, fmt.Errorf("%s arm, case %s: %w", name, gateCase.Name, err)
		}
		arm.Cases = append(arm.Cases, result)
		arm.Total.Money += result.Money
		arm.Total.List += result.List
		if unit == ledger.UnitUnpriced {
			arm.Total.UnpricedCalls++
		}
		arm.TotalInputTokens += result.InputTokens
		if result.Correct {
			arm.CorrectCount++
		}
	}
	if arm.CorrectCount > 0 {
		arm.MoneyPerCorrect = arm.Total.Money / ledger.Money(arm.CorrectCount)
		arm.ListPerCorrect = arm.Total.List / ledger.ListPrice(arm.CorrectCount)
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
	return buildArm("jev", ledger.UnitMoney, cases, func(gateCase benchapi.GateCase) (CaseResult, error) {
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
			Money: ledger.Money(call.Response.Usage.Cost), LatencyMS: msSince(call.Raw.Latency),
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
	return buildArm(name, ledger.UnitMoney, cases, func(gateCase benchapi.GateCase) (CaseResult, error) {
		modelResult, err := wire.Ask(ctx, gateCase.State, battery)
		if err != nil {
			return CaseResult{}, err
		}
		label := Labels[gateCase.Name].Verdict
		if modelResult.Refused {
			return CaseResult{
				Case: gateCase.Name, ModelID: modelResult.Model,
				InputTokens: modelResult.InputTokens, OutputTokens: modelResult.OutputTokens,
				Money: ledger.Money(modelResult.Cost), LatencyMS: msSince(modelResult.Latency),
				Verdict: Refused, Label: label, Correct: false, Refusal: modelResult.Refusal,
			}, nil
		}
		verdict := Decide(modelResult.Answer.UserRequested, modelResult.Answer.Approval)
		return CaseResult{
			Case: gateCase.Name, ModelID: modelResult.Model,
			InputTokens: modelResult.InputTokens, OutputTokens: modelResult.OutputTokens,
			Money: ledger.Money(modelResult.Cost), LatencyMS: msSince(modelResult.Latency),
			UserRequested: modelResult.Answer.UserRequested, Approval: modelResult.Answer.Approval,
			Verdict: verdict, Label: label, Correct: verdict == label,
		}, nil
	})
}

func runRegex(cases []benchapi.GateCase) (ArmResult, error) {
	return buildArm("regex", ledger.UnitMoney, cases, func(gateCase benchapi.GateCase) (CaseResult, error) {
		start := time.Now()
		verdict := RegexDecide(extractCommand(gateCase.State), extractUserMessage(gateCase.State))
		label := Labels[gateCase.Name].Verdict
		return CaseResult{
			Case: gateCase.Name, ModelID: "regexp", LatencyMS: msSince(time.Since(start)),
			Verdict: verdict, Label: label, Correct: verdict == label,
		}, nil
	})
}
