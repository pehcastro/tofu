package sift

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"tofu/internal/judge/jev"
	"tofu/internal/judge/jev/wire/openrouter"
	"tofu/internal/judge/question"
	"tofu/internal/konst"
	"tofu/internal/sift"
	"tofu/internal/transport"
)

const Point = "shell_sift@1"

type Reading struct {
	Session    string
	Command    string
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
	return fmt.Sprintf("%s %-58.58s units %3d candidates %3d needle at %3d %s bytes %6d -> %6d",
		r.Session, r.Command, r.Units, r.Candidates, r.NeedleAt, kept, r.Before, r.After)
}

func read(row Row, planted Planted, marks []sift.Mark) Reading {
	candidates := 0
	for _, unit := range planted.Units {
		if unit.Held == sift.NotHeld {
			candidates++
		}
	}
	return Reading{
		Session:    row.Session,
		Command:    row.Command,
		Units:      len(planted.Units),
		Candidates: candidates,
		NeedleAt:   planted.At,
		NeedleKept: marks[planted.At].Keep,
		Before:     len(sift.JoinUnits(planted.Units)),
		After:      len(sift.Message(planted.Units, marks, sift.ModeEnforced)),
	}
}

func Free(row Row, planted Planted) Reading {
	marks := make([]sift.Mark, len(planted.Units))
	for i, unit := range planted.Units {
		marks[i] = sift.ShellCheap(unit)
	}
	return read(row, planted, marks)
}

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

type reply struct {
	score   float64
	latency time.Duration
	cost    float64
	failed  bool
}

type Answered struct {
	Row       Row
	Planted   Planted
	Scores    map[int]float64
	Latencies []time.Duration
	Cost      float64
	Errors    int
}

func Ask(ctx context.Context, client *jev.Client, set question.Set, row Row, planted Planted) Answered {
	questions := make([]jev.Question, len(set.Questions))
	for i, q := range set.Questions {
		questions[i] = q.ToJev()
	}

	replies := make([]reply, len(planted.Units))
	var wg sync.WaitGroup
	for i, unit := range planted.Units {
		if unit.Held != sift.NotHeld {
			continue
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			replies[i].failed = true
			state, err := json.Marshal(sift.BuildShellState(planted.Shell, planted.Units, i, row.Task))
			if err != nil {
				return
			}
			decision, err := client.Ask(ctx, jev.Request{State: json.RawMessage(state), Questions: questions})
			if err != nil {
				return
			}
			answer, ok := decision.Answers[sift.NeededQuestion]
			if !ok {
				return
			}
			replies[i] = reply{score: answer.Noul, latency: decision.Latency, cost: decision.Usage.Cost}
		}(i)
	}
	wg.Wait()

	out := Answered{Row: row, Planted: planted, Scores: map[int]float64{}}
	for i, unit := range planted.Units {
		if unit.Held != sift.NotHeld {
			continue
		}
		out.Cost += replies[i].cost
		if replies[i].failed {
			out.Errors++
			continue
		}
		out.Latencies = append(out.Latencies, replies[i].latency)
		out.Scores[i] = replies[i].score
	}
	return out
}

func (a Answered) Cut(keepAt float64) Reading {
	marks := make([]sift.Mark, len(a.Planted.Units))
	for i, unit := range a.Planted.Units {
		answers := map[string]float64{}
		if score, ok := a.Scores[i]; ok {
			answers[sift.NeededQuestion] = score
		}
		mark, err := sift.DecideShell(unit, answers, keepAt)
		if err != nil {
			mark = sift.Mark{Keep: true, Reason: "kept: " + err.Error()}
		}
		marks[i] = mark
	}
	return read(a.Row, a.Planted, marks)
}
