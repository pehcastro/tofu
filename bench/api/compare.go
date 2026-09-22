package api

import (
	"context"
	"fmt"
	"time"

	"tofu/bench/stat"
	"tofu/internal/judge/gate"
	"tofu/internal/judge/jev"
	shipped "tofu/library"
)

const gateRuleRef = "tool_gate@1"

type NamedWire struct {
	Name                 string
	Wire                 jev.Wire
	DollarsPerInputToken float64
}

func (n NamedWire) cost(usage jev.Usage) float64 {
	if n.DollarsPerInputToken == 0 {
		return usage.Cost
	}
	return float64(usage.InputTokens) * n.DollarsPerInputToken
}

type WireDecision struct {
	Wire        string
	Build       string
	Verdict     gate.Verdict
	Risk        float64
	MedianMS    float64
	InputTokens int
	Cost        float64
}

type CaseComparison struct {
	Case      string
	Decisions []WireDecision
	Agree     bool
}

func CompareWires(ctx context.Context, wires []NamedWire, repsPerCase int) ([]CaseComparison, error) {
	pol, err := gate.LoadFS(shipped.Files(), gateRuleRef)
	if err != nil {
		return nil, err
	}
	battery, _, err := GateBattery()
	if err != nil {
		return nil, err
	}
	cases, err := GateCases()
	if err != nil {
		return nil, err
	}

	comparisons := make([]CaseComparison, 0, len(cases))
	for _, gateCase := range cases {
		comparison := CaseComparison{Case: gateCase.Name, Agree: true}
		for _, named := range wires {
			latencies := make([]float64, 0, repsPerCase)
			cost := 0.0
			var first Call
			for rep := 0; rep < repsPerCase; rep++ {
				call := Ask(ctx, named.Wire, jev.Request{State: gateCase.State, Questions: battery})
				if call.Err != nil {
					return nil, fmt.Errorf("%s on %s rep %d: %w", named.Name, gateCase.Name, rep, call.Err)
				}
				latencies = append(latencies, float64(call.Raw.Latency)/float64(time.Millisecond))
				cost += named.cost(call.Response.Usage)
				if rep == 0 {
					first = call
				}
			}
			verdict, reason, err := gate.Decide(first.Response.Answers, pol)
			if err != nil {
				return nil, fmt.Errorf("%s on %s: %w", named.Name, gateCase.Name, err)
			}
			comparison.Decisions = append(comparison.Decisions, WireDecision{
				Wire:        named.Name,
				Build:       first.Response.Build,
				Verdict:     verdict,
				Risk:        reason.Value,
				MedianMS:    stat.Median(latencies),
				InputTokens: first.Response.Usage.InputTokens,
				Cost:        cost / float64(repsPerCase),
			})
			if verdict != comparison.Decisions[0].Verdict {
				comparison.Agree = false
			}
		}
		comparisons = append(comparisons, comparison)
	}
	return comparisons, nil
}
