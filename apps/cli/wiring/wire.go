package wiring

import (
	"tofu/internal/agent/loop"
	"tofu/internal/models/llm/catalog"
)

type Wire struct {
	Held     *Accounts
	Spend    loop.Spend
	Selected catalog.Model
}

type Opener func(RunOpts) (Wire, error)

func OpenWire(opts RunOpts) (Wire, error) {
	selected, err := ChooseModel(opts)
	if err != nil {
		return Wire{}, err
	}
	opts.Effort = selected.EffortTaken(opts.Effort)
	held, spend, err := OpenAccounts(opts, selected)
	return Wire{Held: held, Spend: spend, Selected: selected}, err
}
