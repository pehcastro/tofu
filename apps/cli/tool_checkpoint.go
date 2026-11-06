package main

import (
	platformproject "tofu/internal/platform/project"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/files/checkpoint"
	"tofu/internal/tools/history/shadow"
)

func newCheckpointTool(dir string) (contract.Tool, error) {
	places, err := platformproject.ResolvePlaces(dir)
	if err != nil {
		return nil, err
	}
	return checkpoint.Checkpoint{Store: shadow.At(places.State, dir)}, nil
}
