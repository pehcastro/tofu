package recall

import (
	"context"
	"time"

	benchapi "tofu/bench/api"
	"tofu/internal/judge/jev"
)

const checkpointAnswerabilityQuestionID = "answerable"

var checkpointScoreLevels = []string{
	"cannot answer the task from this alone",
	"partly answers it",
	"fully answers it",
}

type CheckpointScore struct {
	Score      float64
	Confidence float64
	Build      string
	LatencyMS  int64
	CostUSD    float64
}

func JevAnswerability(ctx context.Context, wire jev.Wire, task, arm, carry string) (CheckpointScore, error) {
	question := jev.Question{
		ID:           checkpointAnswerabilityQuestionID,
		Kind:         jev.QuestionScore,
		Instructions: "the state carries a task and the carry a forked or checkpointed session would start from. score whether a reader could answer the task from the carry alone, without running anything else.",
		Levels:       checkpointScoreLevels,
	}
	started := time.Now()
	state := map[string]any{"task": task, "arm": arm, "carry": carry}
	call := benchapi.Ask(ctx, wire, jev.Request{State: state, Questions: []jev.Question{question}})
	if call.Err != nil {
		return CheckpointScore{}, call.Err
	}
	elapsed := time.Since(started).Milliseconds()
	answer := call.Response.Answers[checkpointAnswerabilityQuestionID]
	return CheckpointScore{
		Score:      answer.Score,
		Confidence: answer.Confidence,
		Build:      call.Response.Build,
		LatencyMS:  elapsed,
		CostUSD:    call.Response.Usage.Cost,
	}, nil
}
