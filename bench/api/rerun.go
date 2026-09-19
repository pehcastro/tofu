package api

import (
	"context"
	"fmt"

	"boji/bench/stat"
	"boji/internal/judge/jev"
)

const rerunThresholdProbe = 0.5

type RerunQuestion struct {
	ID       string
	Values   []float64
	Min      float64
	Max      float64
	Spread   float64
	Straddle bool
}

func RerunAgreement(ctx context.Context, wire jev.Wire, battery []jev.Question, state any, reruns int) ([]RerunQuestion, []Call, error) {
	values := make(map[string][]float64, len(battery))
	var calls []Call
	for run := 0; run < reruns; run++ {
		call := Ask(ctx, wire, jev.Request{State: state, Questions: battery})
		calls = append(calls, call)
		if call.Err != nil {
			return nil, calls, fmt.Errorf("rerun %d: %w", run, call.Err)
		}
		for _, question := range battery {
			answer := call.Response.Answers[question.ID]
			values[question.ID] = append(values[question.ID], answerValue(answer))
		}
	}
	results := make([]RerunQuestion, 0, len(battery))
	for _, question := range battery {
		v := values[question.ID]
		min, max := stat.Spread(v)
		results = append(results, RerunQuestion{
			ID:       question.ID,
			Values:   v,
			Min:      min,
			Max:      max,
			Spread:   max - min,
			Straddle: min < rerunThresholdProbe && max > rerunThresholdProbe,
		})
	}
	return results, calls, nil
}

func answerValue(a jev.Answer) float64 {
	switch a.Kind {
	case jev.QuestionNoul:
		return a.Noul
	case jev.QuestionScore:
		return a.Score
	case jev.QuestionChoice:
		return a.Confidence
	}
	panic("bench: unknown answer kind")
}
