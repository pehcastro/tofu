package main

import (
	"tofu/internal/tools/agents/done"
	"tofu/internal/tools/contract"
)

func newDoneTool(string) (contract.Tool, error) {
	return done.Done{}, nil
}
