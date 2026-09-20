package recall

import (
	"context"
	"fmt"
	"time"

	benchapi "tofu/bench/api"
	"tofu/internal/judge/jev"
)

const jevWordQuestionID = "carries_the_answer"

type JevWordResult struct {
	Chosen    string
	Build     string
	LatencyMS int64
	CostUSD   float64
}

func jevWordQuestion(candidates []string) jev.Question {
	options := make([]jev.Option, len(candidates))
	for i, candidate := range candidates {
		options[i] = jev.Option{Name: fmt.Sprintf("line_%d", i), Criteria: map[string]string{"text": candidate}}
	}
	return jev.Question{
		ID:           jevWordQuestionID,
		Kind:         jev.QuestionChoice,
		Instructions: "the state carries a task and the candidates a forked session could carry as its last recorded action. choose the option whose text a person would most want kept if only one could survive, to answer the task.",
		Options:      options,
	}
}

func JevLastWord(ctx context.Context, wire jev.Wire, task, raw string) (JevWordResult, error) {
	candidates := lastWordCandidates(raw)
	if len(candidates) <= 1 {
		return JevWordResult{Chosen: raw}, nil
	}
	question := jevWordQuestion(candidates)
	started := time.Now()
	state := map[string]any{"task": task, "candidates": candidates}
	call := benchapi.Ask(ctx, wire, jev.Request{State: state, Questions: []jev.Question{question}})
	if call.Err != nil {
		return JevWordResult{}, call.Err
	}
	elapsed := time.Since(started).Milliseconds()
	answer := call.Response.Answers[jevWordQuestionID]
	index := -1
	for i := range candidates {
		if answer.Choice == fmt.Sprintf("line_%d", i) {
			index = i
			break
		}
	}
	if index < 0 {
		return JevWordResult{}, fmt.Errorf("recall: jev chose %q, which names none of the %d candidates", answer.Choice, len(candidates))
	}
	return JevWordResult{
		Chosen:    candidates[index],
		Build:     call.Response.Build,
		LatencyMS: elapsed,
		CostUSD:   call.Response.Usage.Cost,
	}, nil
}
