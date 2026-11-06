package main

import (
	"time"

	"tofu/internal/konst"
	"tofu/internal/tools/code/eval"
	"tofu/internal/tools/contract"
)

func newEvalTool(dir string) (contract.Tool, error) {
	return eval.New(dir, eval.Limits{
		Run:          konst.EvalRunMillis * time.Millisecond,
		StaleOverlay: konst.OverlayStaleMinutes * time.Minute,
		StdoutLines:  konst.EvalStdoutLines,
		ValueBytes:   konst.EvalValueBytes,
	})
}
