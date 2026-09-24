package main

import (
	"context"
	"encoding/json"
	"slices"
	"sync"

	"tofu/internal/judge/jev"
	"tofu/internal/judge/method"
	"tofu/internal/konst"
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
	turnID  string
	spent   sync.Mutex
	calls   int
	costUSD float64
}

func buildShellSift(arm string) (*turn.ShellSift, *shellScorer, error) {
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
		client, clientErr := newJevClient(konst.SiftConcurrency)
		if clientErr != nil {
			return nil, nil, clientErr
		}
		scorer = &shellScorer{client: client, set: set}
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
	states := make([]json.RawMessage, len(units))
	replies := make([]*jev.Decision, len(units))
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
		asking.Add(1)
		go func() {
			defer asking.Done()
			decision, err := s.client.Ask(ctx, jev.Request{State: states[i], Questions: s.set.Questions})
			if err != nil {
				return
			}
			replies[i] = &decision
		}()
	}
	asking.Wait()

	scores := make(map[int]float64, len(units))
	for i := range units {
		if replies[i] == nil {
			continue
		}
		answer, answered := replies[i].Answers[sift.NeededQuestion]
		if !answered {
			continue
		}
		if err := s.record(states[i], *replies[i]); err != nil {
			return nil, err
		}
		scores[i] = answer.Noul
	}
	return scores, nil
}

func (s *shellScorer) record(state json.RawMessage, decision jev.Decision) error {
	s.spent.Lock()
	s.calls++
	s.costUSD += decision.Usage.Cost
	turnID := s.turnID
	s.spent.Unlock()
	_, err := appendRow(state, s.set, rowInput{
		decision:     &decision,
		answers:      toLedgerAnswers(s.set.QuestionsVersion, decision.Answers),
		turnID:       turnID,
		stateBuilder: shellSiftPoint,
	})
	return err
}

func (s *shellScorer) spend() (int, float64) {
	s.spent.Lock()
	defer s.spent.Unlock()
	return s.calls, s.costUSD
}
