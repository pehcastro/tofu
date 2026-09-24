package readworth

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"tofu/internal/judge/jev"
	"tofu/internal/judge/question"
	"tofu/internal/sift"
)

const Point = "read_worth@1"

type Answered struct {
	Row     Row
	Scores  map[string]float64
	Latency time.Duration
	Cost    float64
	Err     error
}

func Ask(ctx context.Context, client *jev.Client, set question.Set, row Row) Answered {
	questions := make([]jev.Question, len(set.Questions))
	for i, q := range set.Questions {
		questions[i] = q.ToJev()
	}
	state, err := json.Marshal(sift.State{
		Paragraph:   row.Paragraph,
		Task:        row.Task,
		AlreadySaid: row.AlreadySaid,
		Position:    row.Position,
	})
	if err != nil {
		return Answered{Row: row, Err: err}
	}
	decision, err := client.Ask(ctx, jev.Request{State: json.RawMessage(state), Questions: questions})
	if err != nil {
		return Answered{Row: row, Err: err}
	}
	scores := make(map[string]float64, len(decision.Answers))
	for id, answer := range decision.Answers {
		scores[id] = answer.Score
	}
	return Answered{Row: row, Scores: scores, Latency: decision.Latency, Cost: decision.Usage.Cost}
}

func (a Answered) ArmJev() sift.Mark {
	if a.Err != nil {
		return sift.Mark{Keep: true, Reason: "kept: " + a.Err.Error()}
	}
	mark, err := sift.Decide(a.Scores, sift.DefaultRule())
	if err != nil {
		return sift.Mark{Keep: true, Reason: "kept: " + err.Error()}
	}
	return mark
}

func ArmSignpost(row Row) sift.Mark {
	return sift.Signpost(sift.Part{Text: row.Paragraph})
}

func ArmKeepEverything(Row) sift.Mark {
	return sift.Mark{Keep: true}
}

func ArmLength(row Row, minWords int) sift.Mark {
	if len(strings.Fields(row.Paragraph)) < minWords {
		return sift.Mark{Reason: "under the word floor"}
	}
	return sift.Mark{Keep: true}
}
