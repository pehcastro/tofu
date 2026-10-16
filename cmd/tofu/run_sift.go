package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"

	"tofu/internal/judge/jev"
	"tofu/internal/judge/method"
	"tofu/internal/konst"
	"tofu/internal/safe"
	"tofu/internal/sift"
	"tofu/internal/turn"
	shipped "tofu/library"
)

const shellSiftPoint = sift.ShellSchema + "@1"

const (
	siftFollowsTheTable = ""
	siftFree            = "free"
	siftJudged          = "judged"
)

func siftArms() []string { return []string{siftFree, siftJudged} }

func shellSiftCost() string {
	table, err := method.Load(shipped.Files())
	if err != nil {
		return err.Error()
	}
	chosen, err := table.Of(sift.ShellSchema)
	if err != nil {
		return err.Error()
	}
	return chosen.Cost
}

type shellScorer struct {
	client  *jev.Client
	set     battery
	batch   battery
	rubric  jev.Question
	rows    rowLedger
	turnID  string
	spent   sync.Mutex
	calls   int
	costUSD float64
}

func buildShellSift(arm, log string) (*turn.ShellSift, *shellScorer, error) {
	return shellSiftOn(arm, log, jev.ReadKeyring())
}

func shellSiftOn(arm, log string, keys jev.Keyring) (*turn.ShellSift, *shellScorer, error) {
	table, err := method.Load(shipped.Files())
	if err != nil {
		return nil, nil, err
	}
	switch arm {
	case siftFree:
		table = decidedBy(table, method.Cheap)
	case siftJudged:
		table = decidedBy(table, method.Judged)
	}
	chosen, err := table.Of(sift.ShellSchema)
	if err != nil {
		return nil, nil, err
	}
	var scorer *shellScorer
	var scores turn.ShellScores
	if chosen.Method == method.Judged {
		set, setErr := resolveLibrary(shellSiftPoint, "")
		if setErr != nil {
			return nil, nil, setErr
		}
		batch, batchErr := resolveLibrary(sift.ShellBatchPoint, "")
		if batchErr != nil {
			return nil, nil, batchErr
		}
		at := slices.IndexFunc(batch.Questions, func(q jev.Question) bool { return q.ID == sift.NeededQuestion })
		if at < 0 {
			return nil, nil, fmt.Errorf("%s asks no %s, and it is the rubric every chunk is judged by", sift.ShellBatchPoint, sift.NeededQuestion)
		}
		client, clientErr := jevClientFrom(keys, konst.SiftConcurrency)
		if clientErr != nil {
			return nil, nil, clientErr
		}
		rows, rowsErr := openRowLedger(log)
		if rowsErr != nil {
			return nil, nil, rowsErr
		}
		scorer = &shellScorer{client: client, set: set, batch: batch, rubric: batch.Questions[at], rows: rows}
		scores = scorer
	}
	built, err := turn.NewShellSift(scores)
	if err != nil {
		return nil, nil, err
	}
	built.Methods = table
	return &built, scorer, nil
}

func decidedBy(table method.Table, chosen method.Method) method.Table {
	table.Choices = slices.Clone(table.Choices)
	for i, choice := range table.Choices {
		if choice.Point == sift.ShellSchema {
			table.Choices[i].Method = chosen
		}
	}
	return table
}

func (s *shellScorer) Score(ctx context.Context, shell sift.Shell, units []sift.Unit, task string) (map[int]float64, error) {
	batch, err := sift.BuildShellBatch(s.rubric, s.client.Caps(), s.client.Model(), shell, units, task)
	if over := (sift.ShellBatchOverCeilingError{}); errors.As(err, &over) {
		return s.scorePerChunk(ctx, shell, units, task)
	}
	if err != nil {
		return nil, err
	}
	if len(batch.Indexes) == 0 {
		return map[int]float64{}, nil
	}
	decision, err := s.client.Ask(ctx, batch.Request)
	if err != nil {
		return map[int]float64{}, nil
	}
	state, err := json.Marshal(batch.Request.State)
	if err != nil {
		return nil, err
	}
	return batch.Scores(decision), s.record(state, s.batch, sift.ShellBatchPoint, decision)
}

func (s *shellScorer) scorePerChunk(ctx context.Context, shell sift.Shell, units []sift.Unit, task string) (map[int]float64, error) {
	states := make([]json.RawMessage, len(units))
	replies := make([]*jev.Decision, len(units))
	failed := make([]error, len(units))
	var asking sync.WaitGroup
	for i, unit := range units {
		if unit.Held != sift.NotHeld {
			continue
		}
		state, err := json.Marshal(sift.BuildShellState(shell, units, i, task))
		if err != nil {
			return nil, err
		}
		states[i] = state
		asking.Go(func() {
			failed[i] = crashed(".", safe.Call(nil, "shell sift", func() error {
				if decision, err := s.client.Ask(ctx, jev.Request{State: states[i], Questions: s.set.Questions}); err == nil {
					replies[i] = &decision
				}
				return nil
			}))
		})
	}
	asking.Wait()
	if err := errors.Join(failed...); err != nil {
		return nil, err
	}

	scores := make(map[int]float64, len(units))
	for i := range units {
		if replies[i] == nil {
			continue
		}
		answer, answered := replies[i].Answers[sift.NeededQuestion]
		if !answered {
			continue
		}
		if err := s.record(states[i], s.set, shellSiftPoint, *replies[i]); err != nil {
			return nil, err
		}
		scores[i] = answer.Noul
	}
	return scores, nil
}

func (s *shellScorer) record(state json.RawMessage, set battery, builder string, decision jev.Decision) error {
	s.spent.Lock()
	s.calls++
	s.costUSD += decision.Usage.Cost
	turnID := s.turnID
	s.spent.Unlock()
	_, err := s.rows.append(state, set, rowInput{
		decision:     &decision,
		answers:      toLedgerAnswers(set.QuestionsVersion, decision.Answers),
		turnID:       turnID,
		stateBuilder: builder,
	})
	return err
}

func (s *shellScorer) spend() (int, float64) {
	s.spent.Lock()
	defer s.spent.Unlock()
	return s.calls, s.costUSD
}
