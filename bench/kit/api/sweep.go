package api

import (
	"context"
	"time"

	"tofu/internal/models/classifiers/core"
	"tofu/internal/models/classifiers/providers/typesafe"
)

type OptionSweepPoint struct {
	Options     int
	Succeeded   bool
	Correct     bool
	LatencyMS   float64
	BilledInput int
	Confidence  float64
	Cost        float64
	ServerError string
}

func OptionSweep(ctx context.Context, wire core.Wire, counts []int) ([]OptionSweepPoint, []typesafe.Call) {
	results := make([]OptionSweepPoint, 0, len(counts))
	var calls []typesafe.Call
	for _, count := range counts {
		question, expected := OptionSweepQuestion(count)
		state := OptionSweepState(expected)
		call := typesafe.Ask(ctx, wire, core.Request{State: state, Questions: []core.Question{question}})
		calls = append(calls, call)
		if call.Err != nil {
			results = append(results, OptionSweepPoint{Options: count, Succeeded: false, ServerError: call.Err.Error()})
			continue
		}
		answer := call.Response.Answers[question.ID]
		results = append(results, OptionSweepPoint{
			Options:     count,
			Succeeded:   true,
			Correct:     answer.Choice == expected,
			LatencyMS:   float64(call.Raw.Latency) / float64(time.Millisecond),
			BilledInput: call.Response.Usage.InputTokens,
			Confidence:  answer.Confidence,
			Cost:        call.Response.Usage.Cost,
		})
	}
	return results, calls
}
