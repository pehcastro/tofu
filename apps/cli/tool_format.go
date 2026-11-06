package main

import (
	"time"

	"tofu/internal/konst"
	platformproject "tofu/internal/platform/project"
	"tofu/internal/tools/code/format"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/history/shadow"
)

func formatTool(dir string) (contract.Tool, error) {
	places, err := platformproject.ResolvePlaces(dir)
	if err != nil {
		return nil, err
	}
	return format.New(dir, shadow.At(places.State, dir), konst.FormatRunMillis*time.Millisecond)
}
