package main

import (
	"tofu/internal/tools/agents/forget"
	"tofu/internal/tools/contract"
)

func newForgetTool(string) (contract.Tool, error) {
	return forget.Forget{}, nil
}
