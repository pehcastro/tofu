package main

import (
	"time"

	"tofu/internal/konst"
	"tofu/internal/tools/code/mutate"
	"tofu/internal/tools/contract"
)

func newMutateTool(dir string) (contract.Tool, error) {
	return mutate.New(dir, mutate.Limits{
		Check:              konst.WhatifCheckMillis * time.Millisecond,
		StaleOverlay:       konst.OverlayStaleMinutes * time.Minute,
		TimeoutCoefficient: konst.MutateTimeoutCoefficient,
		Mutants:            konst.MutateMutantsCap,
	})
}
