package main

import (
	"tofu/internal/store/session"
	"tofu/internal/store/session/drain"
	"tofu/internal/tools/agents/budget"
	"tofu/internal/tools/contract"
)

func newBudgetTool(dir string) (contract.Tool, error) {
	sessions, err := session.OpenIn(dir)
	if err != nil {
		return nil, err
	}
	drained := func(id string) (drain.Report, error) {
		report, err := sessionDrain(sessions, []string{id})
		return report.Report, err
	}
	input := func() (drain.Input, error) {
		input, _, err := drainInput()
		return input, err
	}
	return budget.Budget{Sessions: sessions, Drain: drained, Input: input}, nil
}
