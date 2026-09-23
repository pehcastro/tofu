package sift

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
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

func Read(row Row, planted Planted, message string) Reading {
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
		NeedleKept: strings.Contains(message, planted.Needle),
		Before:     len(sift.JoinUnits(planted.Units)),
		After:      len(message),
	}
}

func Free(row Row, planted Planted) Reading {
	marks := make([]sift.Mark, len(planted.Units))
	for i, unit := range planted.Units {
		marks[i] = sift.ShellCheap(unit)
	}
	return Read(row, planted, sift.Message(planted.Units, marks, sift.ModeEnforced))
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
	build   string
	failed  bool
}

type Answered struct {
	Units      []sift.Unit
	Scores     map[int]float64
	Latencies  []time.Duration
	Cost       float64
	Errors     int
	StateBytes int
	Builds     map[string]int
	StateSum   string
}

func Ask(ctx context.Context, client *jev.Client, set question.Set, shell sift.Shell, units []sift.Unit, task string) Answered {
	questions := make([]jev.Question, len(set.Questions))
	for i, q := range set.Questions {
		questions[i] = q.ToJev()
	}

	replies := make([]reply, len(units))
	states := make([][]byte, len(units))
	var wg sync.WaitGroup
	for i, unit := range units {
		if unit.Held != sift.NotHeld {
			continue
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			replies[i].failed = true
			state, err := json.Marshal(sift.BuildShellState(shell, units, i, task))
			if err != nil {
				return
			}
			states[i] = state
			decision, err := client.Ask(ctx, jev.Request{State: json.RawMessage(state), Questions: questions})
			if err != nil {
				return
			}
			answer, ok := decision.Answers[sift.NeededQuestion]
			if !ok {
				return
			}
			replies[i] = reply{score: answer.Noul, latency: decision.Latency, cost: decision.Usage.Cost, build: decision.Build}
		}(i)
	}
	wg.Wait()

	out := Answered{Units: units, Scores: map[int]float64{}, Builds: map[string]int{}}
	digest := sha256.New()
	for i, unit := range units {
		if unit.Held != sift.NotHeld {
			continue
		}
		digest.Write(states[i])
		out.Cost += replies[i].cost
		out.StateBytes += len(states[i])
		if replies[i].failed {
			out.Errors++
			continue
		}
		out.Latencies = append(out.Latencies, replies[i].latency)
		out.Scores[i] = replies[i].score
		out.Builds[replies[i].build]++
	}
	out.StateSum = hex.EncodeToString(digest.Sum(nil))
	return out
}

func (a Answered) Cut(keepAt float64) string {
	marks := make([]sift.Mark, len(a.Units))
	for i, unit := range a.Units {
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
	return sift.Message(a.Units, marks, sift.ModeEnforced)
}
