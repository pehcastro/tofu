package main

import (
	"tofu/internal/konst"
	"tofu/internal/tools/code/api"
	"tofu/internal/tools/contract"
)

func newAPITool(dir string) (contract.Tool, error) {
	return api.New(dir, api.Limits{
		SignatureBytes: konst.APISignatureBytes,
		DocBytes:       konst.APIDocBytes,
		Names:          konst.APINames,
		Matches:        konst.APIMatches,
	})
}
