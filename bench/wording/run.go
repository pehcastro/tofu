package wording

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	benchapi "tofu/bench/api"
	"tofu/bench/stat"
	"tofu/internal/judge/jev"
)

const repsPerCase = 3

type answersV1 struct {
	Risk          float64
	Approval      float64
	UserRequested float64
	FromUntrusted float64
}

type answersV2 struct {
	Risk                float64
	Approval            float64
	UserRequested       float64
	MatchesPlanted      float64
	ServesUntrustedAuth float64
}

type CaseResult struct {
	Case                   string
	Runs                   int
	V1                     answersV1
	V2                     answersV2
	V1Min, V1Max           float64
	MatchesMin, MatchesMax float64
	ServesMin, ServesMax   float64
	V1Verdict              bool
	V2Verdict              bool
	VerdictChanged         bool
	Margin                 float64
	V1Cost                 float64
	V2Cost                 float64
}

type Result struct {
	GeneratedAt  time.Time
	Build        string
	Cases        []CaseResult
	ChangedCases []string
	V1TotalCost  float64
	V2TotalCost  float64
	V1TotalCalls int
	V2TotalCalls int
	TotalSpend   float64
}

func Run(ctx context.Context, key string) (Result, error) {
	batteryV1, err := loadBattery(setV1Path)
	if err != nil {
		return Result{}, err
	}
	batteryV2, err := loadBattery(setV2Path)
	if err != nil {
		return Result{}, err
	}
	cases, err := benchapi.GateCases()
	if err != nil {
		return Result{}, err
	}
	wire, err := benchapi.NewWire(key)
	if err != nil {
		return Result{}, err
	}

	result := Result{GeneratedAt: time.Now()}
	for _, gateCase := range cases {
		caseResult, err := runCase(ctx, wire, gateCase, batteryV1, batteryV2)
		if err != nil {
			return Result{}, err
		}
		result.Build = caseResult.build
		result.Cases = append(result.Cases, caseResult.CaseResult)
		result.V1TotalCost += caseResult.V1Cost
		result.V2TotalCost += caseResult.V2Cost
		result.V1TotalCalls += repsPerCase
		result.V2TotalCalls += repsPerCase
		if caseResult.VerdictChanged {
			result.ChangedCases = append(result.ChangedCases, caseResult.Case)
		}
	}
	result.TotalSpend = result.V1TotalCost + result.V2TotalCost
	sort.Strings(result.ChangedCases)
	return result, nil
}

type caseRun struct {
	CaseResult
	build string
}

func runCase(ctx context.Context, wire jev.Wire, gateCase benchapi.GateCase, batteryV1, batteryV2 []jev.Question) (caseRun, error) {
	v1Values := make([]float64, 0, repsPerCase)
	v2MatchValues := make([]float64, 0, repsPerCase)
	v2ServeValues := make([]float64, 0, repsPerCase)
	var v1 answersV1
	var v2 answersV2
	var v1Cost, v2Cost float64
	var build string

	for rep := 0; rep < repsPerCase; rep++ {
		callV1 := benchapi.Ask(ctx, wire, jev.Request{State: gateCase.State, Questions: batteryV1})
		if callV1.Err != nil {
			return caseRun{}, fmt.Errorf("%s v1 rep %d: %w", gateCase.Name, rep, callV1.Err)
		}
		callV2 := benchapi.Ask(ctx, wire, jev.Request{State: gateCase.State, Questions: batteryV2})
		if callV2.Err != nil {
			return caseRun{}, fmt.Errorf("%s v2 rep %d: %w", gateCase.Name, rep, callV2.Err)
		}
		build = callV1.Response.Build
		v1Cost += callV1.Response.Usage.Cost
		v2Cost += callV2.Response.Usage.Cost

		fromUntrusted := callV1.Response.Answers["from_untrusted"].Noul
		v1Values = append(v1Values, fromUntrusted)
		matches := callV2.Response.Answers["matches_planted_instruction"].Noul
		serves := callV2.Response.Answers["serves_untrusted_author"].Noul
		v2MatchValues = append(v2MatchValues, matches)
		v2ServeValues = append(v2ServeValues, serves)

		if rep == 0 {
			v1 = answersV1{
				Risk:          callV1.Response.Answers["risk"].Score,
				Approval:      callV1.Response.Answers["approval"].Noul,
				UserRequested: callV1.Response.Answers["user_requested"].Noul,
				FromUntrusted: fromUntrusted,
			}
			v2 = answersV2{
				Risk:                callV2.Response.Answers["risk"].Score,
				Approval:            callV2.Response.Answers["approval"].Noul,
				UserRequested:       callV2.Response.Answers["user_requested"].Noul,
				MatchesPlanted:      matches,
				ServesUntrustedAuth: serves,
			}
		}
	}

	v1Min, v1Max := stat.Spread(v1Values)
	matchMin, matchMax := stat.Spread(v2MatchValues)
	serveMin, serveMax := stat.Spread(v2ServeValues)

	v1Verdict := verdictV1(v1.FromUntrusted)
	v2Verdict := verdictV2(v2.MatchesPlanted, v2.ServesUntrustedAuth)

	return caseRun{
		build: build,
		CaseResult: CaseResult{
			Case:           gateCase.Name,
			Runs:           repsPerCase,
			V1:             v1,
			V2:             v2,
			V1Min:          v1Min,
			V1Max:          v1Max,
			MatchesMin:     matchMin,
			MatchesMax:     matchMax,
			ServesMin:      serveMin,
			ServesMax:      serveMax,
			V1Verdict:      v1Verdict,
			V2Verdict:      v2Verdict,
			VerdictChanged: v1Verdict != v2Verdict,
			Margin:         math.Abs(v1.FromUntrusted - combinedV2(v2.MatchesPlanted, v2.ServesUntrustedAuth)),
			V1Cost:         v1Cost,
			V2Cost:         v2Cost,
		},
	}, nil
}
