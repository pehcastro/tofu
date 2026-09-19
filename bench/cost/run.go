package cost

import (
	"context"
	"fmt"
	"time"

	benchapi "boji/bench/api"
	"boji/bench/corpus"
	"boji/internal/judge/jev"
	"boji/internal/judge/ledger"
	"boji/internal/judge/policy"
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
	CaughtBlocks     int
	FalseBlocks      int
	Stopped          string
	MoneyPerCorrect  ledger.Money
	ListPerCorrect   ledger.ListPrice
}

type Disagreement struct {
	Arm           string
	Case          string
	Probe         string
	LabelBy       corpus.Labeller
	UserRequested float64
	Approval      float64
	Answer        Verdict
	Label         Verdict
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

type Pair struct {
	Left          string
	Right         string
	LeftOnly      int
	RightOnly     int
	Difference    int
	P             float64
	SeparatedAt05 bool
}

type CorpusCount struct {
	Cases        int
	Recorded     int
	Authored     int
	OwnerLabels  int
	AgentLabels  int
	Blocks       int
	HeldOut      int
	HeldOutBlock int
	SplitAt      string
	SplitMethod  string
}

type Result struct {
	GeneratedAt            time.Time
	Corpus                 CorpusCount
	Calibration            Calibration
	Resolution             policy.Resolution
	Arms                   []ArmResult
	Pairs                  []Pair
	Disagreements          []Disagreement
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
	records, err := corpus.GateRecords()
	if err != nil {
		return Result{}, err
	}
	split, err := corpus.GateSplit()
	if err != nil {
		return Result{}, err
	}
	battery, _, err := benchapi.GateBattery()
	if err != nil {
		return Result{}, err
	}
	calibration, resolution, err := GateCalibration()
	if err != nil {
		return Result{}, err
	}

	byID := make(map[string]corpus.Record, len(records))
	for _, record := range records {
		byID[record.ID] = record
	}
	heldOut := make([]corpus.Record, 0, len(split.Heldout))
	for _, id := range split.Heldout {
		record, ok := byID[id]
		if !ok {
			return Result{}, fmt.Errorf("bench/cost: the split names %s and the corpus does not hold it", id)
		}
		heldOut = append(heldOut, record)
	}

	jevArm, err := runJev(ctx, key, heldOut, battery, calibration.Point)
	if err != nil {
		return Result{}, err
	}
	opusArm, err := runModel(ctx, key, ModelOpus, heldOut, battery, calibration.Point)
	if err != nil {
		return Result{}, err
	}
	fableArm, err := runModel(ctx, key, ModelFable, heldOut, battery, calibration.Point)
	if err != nil {
		return Result{}, err
	}
	regexArm, err := runRegex(heldOut)
	if err != nil {
		return Result{}, err
	}
	floorArm, err := runAlwaysProceed(heldOut)
	if err != nil {
		return Result{}, err
	}

	result := Result{
		GeneratedAt: time.Now(),
		Corpus:      countCorpus(records, heldOut, split),
		Calibration: calibration,
		Resolution:  resolution,
		Arms:        []ArmResult{jevArm, regexArm, opusArm, fableArm, floorArm},
	}
	for _, arm := range result.Arms {
		result.Total = result.Total.Plus(arm.Total)
		for _, c := range arm.Cases {
			if c.Correct {
				continue
			}
			record := byID[c.Case]
			probe, _ := probeOf(record.State)
			result.Disagreements = append(result.Disagreements, Disagreement{
				Arm: arm.Arm, Case: c.Case, Probe: probe, LabelBy: record.LabelBy,
				UserRequested: c.UserRequested, Approval: c.Approval,
				Answer: c.Verdict, Label: c.Label, Refusal: c.Refusal,
			})
		}
	}
	result.Pairs = compare(result.Arms)

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

func verdictOf(label corpus.Label) Verdict {
	if label == corpus.Block {
		return Block
	}
	return Proceed
}

func countCorpus(records, heldOut []corpus.Record, split corpus.Split) CorpusCount {
	count := CorpusCount{Cases: len(records), HeldOut: len(heldOut), SplitAt: split.CreatedAt, SplitMethod: split.Method}
	for _, record := range records {
		if record.Recorded {
			count.Recorded++
		} else {
			count.Authored++
		}
		if record.LabelBy == corpus.Owner {
			count.OwnerLabels++
		} else {
			count.AgentLabels++
		}
		if record.Label == corpus.Block {
			count.Blocks++
		}
	}
	for _, record := range heldOut {
		if record.Label == corpus.Block {
			count.HeldOutBlock++
		}
	}
	return count
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

func buildArm(name string, records []corpus.Record, ask func(corpus.Record) (CaseResult, error)) (ArmResult, error) {
	arm := ArmResult{Arm: name, Unit: ledger.UnitMoney}
	for _, record := range records {
		result, err := ask(record)
		if err != nil {
			arm.Stopped = fmt.Sprintf("stopped at case %s: %v", record.ID, err)
			break
		}
		arm.Cases = append(arm.Cases, result)
		arm.Total.Money += result.Money
		arm.Total.List += result.List
		arm.TotalInputTokens += result.InputTokens
		if result.Correct {
			arm.CorrectCount++
		}
		if result.Verdict == Block && result.Label == Block {
			arm.CaughtBlocks++
		}
		if result.Verdict == Block && result.Label == Proceed {
			arm.FalseBlocks++
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

func runJev(ctx context.Context, key string, records []corpus.Record, battery []jev.Question, point OperatingPoint) (ArmResult, error) {
	wire, err := benchapi.NewWire(key)
	if err != nil {
		return ArmResult{}, err
	}
	return buildArm("jev", records, func(record corpus.Record) (CaseResult, error) {
		call := benchapi.Ask(ctx, wire, jev.Request{State: record.State, Questions: battery})
		if call.Err != nil {
			return CaseResult{}, call.Err
		}
		userRequested := call.Response.Answers["user_requested"].Noul
		approval := call.Response.Answers["approval"].Noul
		verdict := Decide(userRequested, approval, point)
		label := verdictOf(record.Label)
		return CaseResult{
			Case: record.ID, ModelID: call.Response.Build,
			InputTokens: call.Response.Usage.InputTokens, OutputTokens: call.Response.Usage.OutputTokens,
			Money: ledger.Money(call.Response.Usage.Cost), LatencyMS: msSince(call.Raw.Latency),
			UserRequested: userRequested, Approval: approval, Verdict: verdict, Label: label, Correct: verdict == label,
		}, nil
	})
}

func runModel(ctx context.Context, key, model string, records []corpus.Record, battery []jev.Question, point OperatingPoint) (ArmResult, error) {
	wire, err := NewModelWire(key, model)
	if err != nil {
		return ArmResult{}, err
	}
	name := "opus"
	if model == ModelFable {
		name = "fable"
	}
	return buildArm(name, records, func(record corpus.Record) (CaseResult, error) {
		modelResult, err := wire.Ask(ctx, record.State, battery)
		if err != nil {
			return CaseResult{}, err
		}
		label := verdictOf(record.Label)
		if modelResult.Refused {
			return CaseResult{
				Case: record.ID, ModelID: modelResult.Model,
				InputTokens: modelResult.InputTokens, OutputTokens: modelResult.OutputTokens,
				Money: ledger.Money(modelResult.Cost), LatencyMS: msSince(modelResult.Latency),
				Verdict: Refused, Label: label, Correct: false, Refusal: modelResult.Refusal,
			}, nil
		}
		verdict := Decide(modelResult.Answer.UserRequested, modelResult.Answer.Approval, point)
		return CaseResult{
			Case: record.ID, ModelID: modelResult.Model,
			InputTokens: modelResult.InputTokens, OutputTokens: modelResult.OutputTokens,
			Money: ledger.Money(modelResult.Cost), LatencyMS: msSince(modelResult.Latency),
			UserRequested: modelResult.Answer.UserRequested, Approval: modelResult.Answer.Approval,
			Verdict: verdict, Label: label, Correct: verdict == label,
		}, nil
	})
}

func runAlwaysProceed(records []corpus.Record) (ArmResult, error) {
	return buildArm("always-proceed", records, func(record corpus.Record) (CaseResult, error) {
		label := verdictOf(record.Label)
		return CaseResult{
			Case: record.ID, ModelID: "constant",
			Verdict: Proceed, Label: label, Correct: label == Proceed,
		}, nil
	})
}

func runRegex(records []corpus.Record) (ArmResult, error) {
	return buildArm("regex", records, func(record corpus.Record) (CaseResult, error) {
		start := time.Now()
		verdict := RegexDecide(probeOf(record.State))
		label := verdictOf(record.Label)
		return CaseResult{
			Case: record.ID, ModelID: "regexp", LatencyMS: msSince(time.Since(start)),
			Verdict: verdict, Label: label, Correct: verdict == label,
		}, nil
	})
}
