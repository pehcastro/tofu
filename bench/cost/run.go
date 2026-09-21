package cost

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	benchapi "tofu/bench/api"
	"tofu/bench/corpus"
	"tofu/internal/judge/gate"
	"tofu/internal/judge/jev"
	"tofu/internal/judge/ledger"
)

type CaseResult struct {
	Case         string
	ModelID      string
	InputTokens  int
	OutputTokens int
	Money        ledger.Money
	List         ledger.ListPrice
	LatencyMS    float64
	Answers      map[string]jev.Answer
	Reason       gate.Reason
	Verdict      Verdict
	Label        Verdict
	Correct      bool
	Refusal      string
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
	Arm     string
	Case    string
	Probe   string
	LabelBy corpus.Labeller
	Answers map[string]jev.Answer
	Reason  gate.Reason
	Answer  Verdict
	Label   Verdict
	Refusal string
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
	GeneratedAt          time.Time
	Corpus               CorpusCount
	Rule                 gate.Rule
	Resolution           gate.Resolution
	AnswersFile          string
	Arms                 []ArmResult
	Pairs                []Pair
	Disagreements        []Disagreement
	ContextTokensAvoided int
	JevMoneyPerCorrect   ledger.Money
	JevTokensPerCorrect  int
	Total                ledger.Spend
}

func Run(ctx context.Context, key string) (Result, error) {
	root, err := os.Getwd()
	if err != nil {
		return Result{}, err
	}
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
	pol, resolution, err := gateRule(root)
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

	jevArm, err := runJev(ctx, key, heldOut, battery, pol)
	if err != nil {
		return Result{}, err
	}

	result := Result{
		GeneratedAt: time.Now(),
		Corpus:      countCorpus(records, heldOut, split),
		Rule:        pol,
		Resolution:  resolution,
		Arms:        []ArmResult{jevArm, runRegex(heldOut), runAlwaysProceed(heldOut)},
	}
	result.AnswersFile = answersFilename(result.GeneratedAt)
	answersSource := fmt.Sprintf("the paid run of %s, decided through %s", result.GeneratedAt.Format(time.RFC3339), pol.File)
	if err := writeAnswers(filepath.Join(root, "bench", "cost", result.AnswersFile), answerRows(result.Arms, pol, answersSource)); err != nil {
		return Result{}, err
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
				Answers: c.Answers, Reason: c.Reason,
				Answer: c.Verdict, Label: c.Label, Refusal: c.Refusal,
			})
		}
	}
	result.Pairs = compare(result.Arms)

	result.ContextTokensAvoided = jevArm.TotalInputTokens
	result.JevMoneyPerCorrect = jevArm.MoneyPerCorrect
	if jevArm.CorrectCount > 0 {
		result.JevTokensPerCorrect = jevArm.TotalInputTokens / jevArm.CorrectCount
	}

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

func buildArm(name string, records []corpus.Record, ask func(corpus.Record) (CaseResult, error)) ArmResult {
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
	return arm
}

func msSince(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}

func runJev(ctx context.Context, key string, records []corpus.Record, battery []jev.Question, pol gate.Rule) (ArmResult, error) {
	wire, err := benchapi.NewWire(key)
	if err != nil {
		return ArmResult{}, err
	}
	arm := buildArm("jev", records, func(record corpus.Record) (CaseResult, error) {
		call := benchapi.Ask(ctx, wire, jev.Request{State: record.State, Questions: battery})
		if call.Err != nil {
			return CaseResult{}, call.Err
		}
		verdict, reason, err := decide(call.Response.Answers, pol)
		if err != nil {
			return CaseResult{}, err
		}
		label := verdictOf(record.Label)
		return CaseResult{
			Case: record.ID, ModelID: call.Response.Build,
			InputTokens: call.Response.Usage.InputTokens, OutputTokens: call.Response.Usage.OutputTokens,
			Money: ledger.Money(call.Response.Usage.Cost), LatencyMS: msSince(call.Raw.Latency),
			Answers: call.Response.Answers, Reason: reason,
			Verdict: verdict, Label: label, Correct: verdict == label,
		}, nil
	})
	return arm, nil
}

const (
	modelRegex    = "regexp"
	modelConstant = "constant"
)

func runAlwaysProceed(records []corpus.Record) ArmResult {
	return buildArm("always-proceed", records, func(record corpus.Record) (CaseResult, error) {
		label := verdictOf(record.Label)
		return CaseResult{
			Case: record.ID, ModelID: modelConstant,
			Verdict: Proceed, Label: label, Correct: label == Proceed,
		}, nil
	})
}

func runRegex(records []corpus.Record) ArmResult {
	return buildArm("regex", records, func(record corpus.Record) (CaseResult, error) {
		start := time.Now()
		verdict := RegexDecide(probeOf(record.State))
		label := verdictOf(record.Label)
		return CaseResult{
			Case: record.ID, ModelID: modelRegex, LatencyMS: msSince(time.Since(start)),
			Verdict: verdict, Label: label, Correct: verdict == label,
		}, nil
	})
}
