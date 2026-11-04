package wiring

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"

	"tofu/internal/agent/loop"
	"tofu/internal/judge/method"
	"tofu/internal/konst"
	"tofu/internal/models/classifiers/core"
	"tofu/internal/platform/recover"
	"tofu/internal/sys"
	"tofu/internal/tools/shell/sift"
	shipped "tofu/library"
)

const ShellSiftPoint = sift.ShellSchema + "@1"

const (
	SiftFollowsTheTable = ""
	SiftFree            = "free"
	SiftJudged          = "judged"
)

func SiftArms() []string { return []string{SiftFree, SiftJudged} }

func ShellSiftCost() string {
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

type ShellScorer struct {
	client  *core.Client
	set     Battery
	batch   Battery
	rubric  core.Question
	rows    RowLedger
	TurnID  string
	spent   sync.Mutex
	calls   int
	costUSD float64
}

func BuildShellSift(arm, log string) (*loop.ShellSift, *ShellScorer, error) {
	return ShellSiftOn(arm, log, core.ReadKeyring())
}

func ShellSiftOn(arm, log string, keys core.Keyring) (*loop.ShellSift, *ShellScorer, error) {
	table, err := method.Load(shipped.Files())
	if err != nil {
		return nil, nil, err
	}
	switch arm {
	case SiftFree:
		table = decidedBy(table, method.Cheap)
	case SiftJudged:
		table = decidedBy(table, method.Judged)
	}
	chosen, err := table.Of(sift.ShellSchema)
	if err != nil {
		return nil, nil, err
	}
	var scorer *ShellScorer
	var scores loop.ShellScores
	if chosen.Method == method.Judged {
		set, setErr := ResolveLibrary(ShellSiftPoint, "")
		if setErr != nil {
			return nil, nil, setErr
		}
		batch, batchErr := ResolveLibrary(sift.ShellBatchPoint, "")
		if batchErr != nil {
			return nil, nil, batchErr
		}
		at := slices.IndexFunc(batch.Questions, func(q core.Question) bool { return q.ID == sift.NeededQuestion })
		if at < 0 {
			return nil, nil, fmt.Errorf("%s asks no %s, and it is the rubric every chunk is judged by", sift.ShellBatchPoint, sift.NeededQuestion)
		}
		client, clientErr := JevClientFrom(keys, konst.SiftConcurrency)
		if clientErr != nil {
			return nil, nil, clientErr
		}
		rows, rowsErr := OpenRowLedger(log)
		if rowsErr != nil {
			return nil, nil, rowsErr
		}
		scorer = &ShellScorer{client: client, set: set, batch: batch, rubric: batch.Questions[at], rows: rows}
		scores = scorer
	}
	built, err := loop.NewShellSift(scores)
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

func (s *ShellScorer) Score(ctx context.Context, shell sift.Shell, units []sift.Unit, task string) (map[int]float64, error) {
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

func (s *ShellScorer) scorePerChunk(ctx context.Context, shell sift.Shell, units []sift.Unit, task string) (map[int]float64, error) {
	states := make([]json.RawMessage, len(units))
	replies := make([]*core.Decision, len(units))
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
			failed[i] = sys.Crashed(".", recover.Call(nil, "shell sift", func() error {
				if decision, err := s.client.Ask(ctx, core.Request{State: states[i], Questions: s.set.Questions}); err == nil {
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
		if err := s.record(states[i], s.set, ShellSiftPoint, *replies[i]); err != nil {
			return nil, err
		}
		scores[i] = answer.Noul
	}
	return scores, nil
}

func (s *ShellScorer) record(state json.RawMessage, set Battery, builder string, decision core.Decision) error {
	s.spent.Lock()
	turnID := s.TurnID
	s.spent.Unlock()
	row, err := s.rows.Append(state, set, RowInput{
		Decision:     &decision,
		Answers:      ToLedgerAnswers(set.QuestionsVersion, decision.Answers),
		TurnID:       turnID,
		StateBuilder: builder,
	})
	s.spent.Lock()
	s.calls++
	s.costUSD += row.Charge.USD
	s.spent.Unlock()
	return err
}

func (s *ShellScorer) Spend() (int, float64) {
	s.spent.Lock()
	defer s.spent.Unlock()
	return s.calls, s.costUSD
}
