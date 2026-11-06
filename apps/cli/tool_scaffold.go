package main

import (
	"tofu/internal/tools/code/scaffold"
	"tofu/internal/tools/contract"
)

func newScaffoldTool(dir string) (contract.Tool, error) {
	return scaffold.New(dir)
}
