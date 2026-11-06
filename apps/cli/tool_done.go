package main

import (
	"tofu/internal/judge/ledger"
	"tofu/internal/models/classifiers"
	jev "tofu/internal/models/classifiers/core"
	platformproject "tofu/internal/platform/project"
	"tofu/internal/tools/agents/done"
	"tofu/internal/tools/contract"
)

func newDoneTool(dir string) (contract.Tool, error) {
	places, err := platformproject.ResolvePlaces(dir)
	if err != nil {
		return nil, err
	}
	return done.Done{
		Dir:    dir,
		Ledger: ledger.NewWriter(places.Log),
		Open:   func() (*jev.Client, error) { return classifiers.Open(dir, jev.ReadKeyring(), 1) },
	}, nil
}
