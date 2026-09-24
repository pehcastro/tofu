package websift

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"tofu/internal/judge/jev"
	"tofu/internal/judge/jev/wire/openrouter"
	"tofu/internal/judge/question"
	"tofu/internal/sift"
)

const Point = "page_sift@1"

type Reading struct {
	Source     string
	URL        string
	Units      int
	Candidates int
	NeedleAt   int
	NeedleKept bool
	Before     int
	After      int
}

func (r Reading) Line() string {
	kept := "lost"
	if r.NeedleKept {
		kept = "kept"
	}
	return fmt.Sprintf("%-8.8s %-52.52s units %3d candidates %3d needle at %3d %s bytes %6d -> %6d",
		r.Source, r.URL, r.Units, r.Candidates, r.NeedleAt, kept, r.Before, r.After)
}

func read(row Row, planted Planted, marks []sift.Mark) Reading {
	candidates := 0
	for _, unit := range planted.Units {
		if unit.Held == sift.NotHeld {
			candidates++
		}
	}
	return Reading{
		Source:     row.Source,
		URL:        planted.URL,
		Units:      len(planted.Units),
		Candidates: candidates,
		NeedleAt:   planted.At,
		NeedleKept: marks[planted.At].Keep,
		Before:     len(sift.JoinPageUnits(planted.Units)),
		After:      len(sift.PageMessage(planted.Units, marks, sift.ModeEnforced)),
	}
}

func Free(row Row, planted Planted) Reading {
	marks := make([]sift.Mark, len(planted.Units))
	for i, unit := range planted.Units {
		marks[i] = sift.PageCheap(unit)
	}
	return read(row, planted, marks)
}

func KeepEverything(row Row, planted Planted) Reading {
	marks := make([]sift.Mark, len(planted.Units))
	for i := range marks {
		marks[i] = sift.Mark{Keep: true, Reason: "the keep-everything arm keeps everything"}
	}
	return read(row, planted, marks)
}

type Answered struct {
	Row       Row
	Planted   Planted
	Scores    map[int]float64
	Latencies []time.Duration
	Cost      float64
	Chunks    int
	Failed    int
}

func chunkCandidates(template jev.Question, candidates []sift.PageUnit, task, pageURL string, budget int) [][]sift.PageUnit {
	var chunks [][]sift.PageUnit
	var current []sift.PageUnit
	for _, unit := range candidates {
		trial := append(append([]sift.PageUnit{}, current...), unit)
		if len(current) > 0 && requestSize(template, trial, task, pageURL) > budget {
			chunks = append(chunks, current)
			current = []sift.PageUnit{unit}
			continue
		}
		current = trial
	}
	if len(current) > 0 {
		chunks = append(chunks, current)
	}
	return chunks
}

func requestSize(template jev.Question, units []sift.PageUnit, task, pageURL string) int {
	state, err := json.Marshal(sift.BuildPageState(units, task, pageURL))
	if err != nil {
		return 0
	}
	questions := make([]jev.Question, len(units))
	for i, unit := range units {
		q := template
		q.ID = sift.PageQuestionID(unit.Index)
		questions[i] = q
	}
	body, err := (jev.Request{State: json.RawMessage(state), Questions: questions}).Encode(openrouter.Alias)
	if err != nil {
		return 0
	}
	return len(body)
}

func Ask(ctx context.Context, client *jev.Client, set question.Set, row Row, planted Planted) Answered {
	template := set.Questions[0].ToJev()
	var candidates []sift.PageUnit
	for _, unit := range planted.Units {
		if unit.Held == sift.NotHeld {
			candidates = append(candidates, unit)
		}
	}
	out := Answered{Row: row, Planted: planted, Scores: map[int]float64{}}
	if len(candidates) == 0 {
		return out
	}

	budget := client.Caps().MaxRequestBytes
	for _, chunk := range chunkCandidates(template, candidates, row.Task, planted.URL, budget) {
		out.Chunks++
		state, err := json.Marshal(sift.BuildPageState(chunk, row.Task, planted.URL))
		if err != nil {
			out.Failed++
			continue
		}
		questions := make([]jev.Question, len(chunk))
		for i, unit := range chunk {
			q := template
			q.ID = sift.PageQuestionID(unit.Index)
			questions[i] = q
		}
		decision, err := client.Ask(ctx, jev.Request{State: json.RawMessage(state), Questions: questions})
		if err != nil {
			out.Failed++
			continue
		}
		out.Latencies = append(out.Latencies, decision.Latency)
		out.Cost += decision.Usage.Cost
		for _, unit := range chunk {
			if answer, ok := decision.Answers[sift.PageQuestionID(unit.Index)]; ok {
				out.Scores[unit.Index] = answer.Noul
			}
		}
	}
	return out
}

func (a Answered) Cut(keepAt float64) Reading {
	marks := make([]sift.Mark, len(a.Planted.Units))
	for i, unit := range a.Planted.Units {
		score, ok := a.Scores[i]
		mark, err := sift.DecidePage(unit, score, ok, keepAt)
		if err != nil {
			mark = sift.Mark{Keep: true, Reason: "kept: " + err.Error()}
		}
		marks[i] = mark
	}
	return read(a.Row, a.Planted, marks)
}
