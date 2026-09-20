package shortlist

import (
	"context"
	"fmt"
	"strings"
	"time"

	"tofu/internal/judge/jev"
	"tofu/internal/judge/jev/wire/openrouter"
	"tofu/internal/konst"
	"tofu/internal/transport"
)

const (
	ShortlistSize = 5
	headLines     = 20
	none          = "__none__"
)

func Shortlist(files []File, ranked []Ranked, size int) []File {
	if size > len(ranked) {
		size = len(ranked)
	}
	index := make(map[string]File, len(files))
	for _, f := range files {
		index[f.Path] = f
	}
	out := make([]File, 0, size)
	for _, r := range ranked[:size] {
		if f, ok := index[r.Path]; ok {
			out = append(out, f)
		}
	}
	return out
}

func head(content string, lines int) string {
	split := strings.SplitN(content, "\n", lines+1)
	if len(split) > lines {
		split = split[:lines]
	}
	return strings.Join(split, "\n")
}

func candidateID(i int) string { return fmt.Sprintf("candidate_%d", i) }

func NewWire(key string) (*openrouter.Wire, error) {
	return openrouter.New(openrouter.Config{
		Key: key,
		Transport: transport.Config{
			AttemptTimeout: time.Duration(konst.JudgeTimeoutMillis) * time.Millisecond,
			Retries:        konst.JudgeRetries,
			Backoff:        time.Duration(konst.JudgeBackoffMillis) * time.Millisecond,
			Concurrency:    konst.SiftConcurrency,
		},
	})
}

type JudgedAnswer struct {
	Ranked   []Ranked
	Choice   string
	Build    string
	Latency  time.Duration
	Cost     float64
	Attempts int
}

func buildRequest(task string, shortlist []File) jev.Request {
	candidates := make([]map[string]any, len(shortlist))
	options := make([]jev.Option, 0, len(shortlist)+1)
	questions := make([]jev.Question, 0, len(shortlist)+1)
	for i, f := range shortlist {
		candidates[i] = map[string]any{
			"path": f.Path,
			"head": head(f.Content, headLines),
		}
		options = append(options, jev.Option{Name: f.Path, Criteria: map[string]any{"see": fmt.Sprintf("candidates[%d]", i)}})
		questions = append(questions, jev.Question{
			ID:           candidateID(i),
			Kind:         jev.QuestionNoul,
			Instructions: fmt.Sprintf("Is `candidates[%d]` the file that `query` refers to?", i),
			True:         "this file is what query refers to; its path and head confirm it",
			False:        "this file is not the target; at most it is related by topic",
		})
	}
	options = append(options, jev.Option{Name: none, Criteria: "none of the candidates is the file referred to"})
	questions = append(questions, jev.Question{
		ID:           "best",
		Kind:         jev.QuestionChoice,
		Instructions: "Which candidate is the file that `query` refers to?",
		Options:      options,
	})
	return jev.Request{
		State:     map[string]any{"query": task, "candidates": candidates},
		Questions: questions,
	}
}

func AskJudged(ctx context.Context, client *jev.Client, task string, shortlist []File) (JudgedAnswer, error) {
	decision, err := client.Ask(ctx, buildRequest(task, shortlist))
	if err != nil {
		return JudgedAnswer{}, err
	}
	ranked := make([]Ranked, len(shortlist))
	for i, f := range shortlist {
		answer, ok := decision.Answers[candidateID(i)]
		score := 0.0
		if ok {
			score = answer.Noul
		}
		ranked[i] = Ranked{Path: f.Path, Score: score}
	}
	sortRanked(ranked)
	choice := ""
	if best, ok := decision.Answers["best"]; ok {
		choice = best.Choice
	}
	return JudgedAnswer{
		Ranked:   ranked,
		Choice:   choice,
		Build:    decision.Build,
		Latency:  decision.Latency,
		Cost:     decision.Usage.Cost,
		Attempts: decision.Attempts,
	}, nil
}
