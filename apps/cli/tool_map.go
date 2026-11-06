package main

import (
	"tofu/internal/konst"
	repomap "tofu/internal/tools/code/map"
	"tofu/internal/tools/contract"
)

func newMapTool(dir string) (contract.Tool, error) {
	return repomap.New(dir, repomap.Limits{
		Budget:         konst.MapBudgetTokens,
		BytesPerToken:  konst.SearchBytesPerToken,
		Damping:        konst.MapRankDamping,
		Iterations:     konst.MapRankIterations,
		MentionWeight:  konst.MapMentionWeight,
		SignatureBytes: konst.MapSignatureBytes,
	})
}
