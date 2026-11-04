package api

import (
	"context"
	"fmt"
	"time"

	"tofu/internal/models/classifiers/core"
	"tofu/internal/models/classifiers/providers/typesafe"
	"tofu/internal/stat"
)

type SizeLatency struct {
	Label       string
	Runs        int
	MedianMS    float64
	P95MS       float64
	P99MS       float64
	MinMS       float64
	MaxMS       float64
	BilledInput []int
	Build       string
	Cost        float64
}

func LatencyByStateSize(ctx context.Context, wire core.Wire, fixtures []SizeFixture, question core.Question, runsPerSize int) ([]SizeLatency, []typesafe.Call, error) {
	results := make([]SizeLatency, 0, len(fixtures))
	var calls []typesafe.Call
	for _, fixture := range fixtures {
		latencies := make([]float64, 0, runsPerSize)
		billed := make([]int, 0, runsPerSize)
		build := ""
		cost := 0.0
		for run := 0; run < runsPerSize; run++ {
			call := typesafe.Ask(ctx, wire, core.Request{State: fixture.State, Questions: []core.Question{question}})
			calls = append(calls, call)
			if call.Err != nil {
				return nil, calls, fmt.Errorf("size %s run %d: %w", fixture.Label, run, call.Err)
			}
			latencies = append(latencies, float64(call.Raw.Latency)/float64(time.Millisecond))
			billed = append(billed, call.Response.Usage.InputTokens)
			build = call.Response.Build
			cost += call.Response.Usage.Cost
		}
		min, max := stat.Spread(latencies)
		results = append(results, SizeLatency{
			Label:       fixture.Label,
			Runs:        runsPerSize,
			MedianMS:    stat.Median(latencies),
			P95MS:       stat.Percentile(latencies, 95),
			P99MS:       stat.Percentile(latencies, 99),
			MinMS:       min,
			MaxMS:       max,
			BilledInput: billed,
			Build:       build,
			Cost:        cost,
		})
	}
	return results, calls, nil
}

type CountLatency struct {
	Count    int
	Runs     int
	MedianMS float64
}

func LatencyByQuestionCount(ctx context.Context, wire core.Wire, state any, counts []int, runsPerCount int) ([]CountLatency, []typesafe.Call, error) {
	results := make([]CountLatency, 0, len(counts))
	var calls []typesafe.Call
	for _, count := range counts {
		questions := CountSweepQuestions(count)
		latencies := make([]float64, 0, runsPerCount)
		for run := 0; run < runsPerCount; run++ {
			call := typesafe.Ask(ctx, wire, core.Request{State: state, Questions: questions})
			calls = append(calls, call)
			if call.Err != nil {
				return nil, calls, fmt.Errorf("question count %d run %d: %w", count, run, call.Err)
			}
			latencies = append(latencies, float64(call.Raw.Latency)/float64(time.Millisecond))
		}
		results = append(results, CountLatency{Count: count, Runs: runsPerCount, MedianMS: stat.Median(latencies)})
	}
	return results, calls, nil
}
