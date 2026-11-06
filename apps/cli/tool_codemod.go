package main

import (
	"tofu/internal/tools/code/codemod"
	"tofu/internal/tools/contract"
)

func codemodTool(dir string) (contract.Tool, error) {
	return codemod.New(dir)
}
