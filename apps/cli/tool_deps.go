package main

import (
	"time"

	"tofu/internal/konst"
	"tofu/internal/tools/code/deps"
	"tofu/internal/tools/contract"
)

func depsTool(dir string) (contract.Tool, error) {
	return deps.New(dir, deps.Limits{
		Request:    konst.DepsRequestMillis * time.Millisecond,
		Run:        konst.DepsRunMillis * time.Millisecond,
		Advisories: konst.DepsAdvisoriesShown,
		Lines:      konst.DepsOutputLines,
	})
}
