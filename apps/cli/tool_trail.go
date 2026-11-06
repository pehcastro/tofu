package main

import (
	platformproject "tofu/internal/platform/project"
	"tofu/internal/store/session"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/history/shadow"
	"tofu/internal/tools/history/trail"
)

func newTrailTool(dir string) (contract.Tool, error) {
	places, err := platformproject.ResolvePlaces(dir)
	if err != nil {
		return nil, err
	}
	return trail.Trail{Store: shadow.At(places.State, dir), Sessions: session.OpenAt(places.State)}, nil
}
