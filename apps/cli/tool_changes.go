package main

import (
	platformproject "tofu/internal/platform/project"
	"tofu/internal/store/session"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/history/changes"
	"tofu/internal/tools/history/shadow"
)

func newChangesTool(dir string) (contract.Tool, error) {
	places, err := platformproject.ResolvePlaces(dir)
	if err != nil {
		return nil, err
	}
	return changes.Changes{Store: shadow.At(places.State, dir), Sessions: session.OpenAt(places.State)}, nil
}
