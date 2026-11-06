package main

import (
	repomap "tofu/internal/tools/code/map"
	"tofu/internal/tools/contract"
)

func newMapTool(dir string) (contract.Tool, error) {
	return repomap.New(dir)
}
