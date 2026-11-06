package main

import (
	"time"

	"tofu/internal/konst"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/scratch/whatif"
)

func newWhatifTool(dir string) (contract.Tool, error) {
	return whatif.New(dir, whatif.Limits{Check: konst.WhatifCheckMillis * time.Millisecond, StaleOverlay: konst.OverlayStaleMinutes * time.Minute})
}
