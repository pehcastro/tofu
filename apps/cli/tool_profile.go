package main

import (
	"time"

	"tofu/internal/konst"
	"tofu/internal/tools/code/profile"
	"tofu/internal/tools/contract"
)

func profileTool(dir string) (contract.Tool, error) {
	return profile.New(dir, profile.Limits{
		Run:   konst.ProfileRunMillis * time.Millisecond,
		Rows:  konst.ProfileRows,
		CPUs:  konst.TestCPUs,
		Lines: konst.ProfileOutputLines,
	})
}
