package main

import (
	"time"

	shellsverb "tofu/apps/cli/verbs/shells"
	"tofu/internal/konst"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/web/http"
)

func newHTTPTool(dir string) (contract.Tool, error) {
	shells, err := shellsverb.LaunchShellRegistry(dir)
	if err != nil {
		return nil, err
	}
	return http.New(dir, shells, http.Limits{
		Read:         konst.HTTPReadBytes,
		Shown:        konst.HTTPShownBytes,
		Items:        konst.HTTPItemsShown,
		Call:         konst.HTTPCallMillis * time.Millisecond,
		ShapeContext: konst.ShapeErrorContextBytes,
	})
}
