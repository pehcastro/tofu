package main

import (
	"time"

	"tofu/internal/konst"
	platformproject "tofu/internal/platform/project"
	"tofu/internal/store/session"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/history/bisect"
	"tofu/internal/tools/history/shadow"
	"tofu/internal/tools/scratch/whatif"
)

func newBisectTool(dir string) (contract.Tool, error) {
	places, err := platformproject.ResolvePlaces(dir)
	if err != nil {
		return nil, err
	}
	return bisect.New(dir, shadow.At(places.State, dir), session.OpenAt(places.State), whatif.Limits{Check: konst.WhatifCheckMillis * time.Millisecond, StaleOverlay: konst.OverlayStaleMinutes * time.Minute})
}
