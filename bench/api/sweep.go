package api

import (
	"context"
	"time"

	"boji/internal/judge/jev"
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

func OptionSweep(ctx context.Context, wire jev.Wire, counts []int) ([]OptionSweepPoint, []Call) {
	results := make([]OptionSweepPoint, 0, len(counts))
	var calls []Call
	for _, count := range counts {
		question, expected := OptionSweepQuestion(count)
		state := OptionSweepState(expected)
		call := Ask(ctx, wire, jev.Request{State: state, Questions: []jev.Question{question}})
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
