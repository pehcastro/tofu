package main

import (
	"tofu/internal/tools/contract"
	"tofu/internal/tools/shell/env"
)

func envTool(dir string) (contract.Tool, error) {
	return env.New(dir)
}
