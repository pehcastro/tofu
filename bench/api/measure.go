package api

import (
	"context"
	"fmt"
	"time"

	"tofu/bench/stat"
	"tofu/internal/judge/jev"
)

type GateCaseResult struct {
	Name      string
	Answers   map[string]float64
	LatencyMS float64
}

type Result struct {
	Build           string
	GeneratedAt     time.Time
	SizeLatencies   []SizeLatency
	CountLatencies  []CountLatency
	GateCases       []GateCaseResult
	GateMedianMS    float64
	GateP95MS       float64
	GateP99MS       float64
	GateMinMS       float64
	GateMaxMS       float64
	GateSampleCount int
	RerunResults    []RerunQuestion
	RerunCase       string
	OptionSweep     []OptionSweepPoint
	TotalCost       float64
	TotalCalls      int
}

func Run(ctx context.Context, wire jev.Wire) (Result, error) {
	result := Result{GeneratedAt: time.Now()}
	var allCalls []Call

	battery, _, err := GateBattery()
	if err != nil {
		return Result{}, err
	}
	cases, err := GateCases()
	if err != nil {
		return Result{}, err
	}
	const gateRepsPerCase = 3
	gateLatencies := make([]float64, 0, len(cases)*gateRepsPerCase)
	for _, gateCase := range cases {
		for rep := 0; rep < gateRepsPerCase; rep++ {
			call := Ask(ctx, wire, jev.Request{State: gateCase.State, Questions: battery})
			allCalls = append(allCalls, call)
			if call.Err != nil {
				return Result{}, fmt.Errorf("gate case %s rep %d: %w", gateCase.Name, rep, call.Err)
			}
			latencyMS := float64(call.Raw.Latency) / float64(time.Millisecond)
			gateLatencies = append(gateLatencies, latencyMS)
			result.Build = call.Response.Build
			if rep > 0 {
				continue
			}
			answers := make(map[string]float64, len(call.Response.Answers))
			for id, answer := range call.Response.Answers {
				answers[id] = answerValue(answer)
			}
			result.GateCases = append(result.GateCases, GateCaseResult{Name: gateCase.Name, Answers: answers, LatencyMS: latencyMS})
		}
	}
	min, max := stat.Spread(gateLatencies)
	result.GateMedianMS = stat.Median(gateLatencies)
	result.GateP95MS = stat.Percentile(gateLatencies, 95)
	result.GateP99MS = stat.Percentile(gateLatencies, 99)
	result.GateMinMS = min
	result.GateMaxMS = max
	result.GateSampleCount = len(gateLatencies)

	fixtures, err := SizeFixtures()
	if err != nil {
		return Result{}, err
	}
	sizeLatencies, sizeCalls, err := LatencyByStateSize(ctx, wire, fixtures, countSweepPool[0], 3)
	if err != nil {
		return Result{}, err
	}
	result.SizeLatencies = sizeLatencies
	allCalls = append(allCalls, sizeCalls...)

	countState := fixtures[1].State
	countLatencies, countCalls, err := LatencyByQuestionCount(ctx, wire, countState, []int{1, 4, 12}, 3)
	if err != nil {
		return Result{}, err
	}
	result.CountLatencies = countLatencies
	allCalls = append(allCalls, countCalls...)

	rerunCase := cases[5]
	rerunResults, rerunCalls, err := RerunAgreement(ctx, wire, battery, rerunCase.State, 5)
	if err != nil {
		return Result{}, err
	}
	result.RerunResults = rerunResults
	result.RerunCase = rerunCase.Name
	allCalls = append(allCalls, rerunCalls...)

	sweepResults, sweepCalls := OptionSweep(ctx, wire, []int{8, 64, 255, 256})
	result.OptionSweep = sweepResults
	allCalls = append(allCalls, sweepCalls...)

	for _, call := range allCalls {
		result.TotalCalls++
		result.TotalCost += call.Response.Usage.Cost
	}
	return result, nil
}
