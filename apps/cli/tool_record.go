package main

import (
	"time"

	"tofu/internal/konst"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/web/record"
)

func recordTool(dir string) (contract.Tool, error) {
	return record.New(dir, record.Limits{
		Run:   konst.RecordRunMillis * time.Millisecond,
		Body:  konst.RecordBodyBytes,
		Lines: konst.RecordOutputLines,
	})
}
