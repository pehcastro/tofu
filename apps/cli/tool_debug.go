package main

import (
	"time"

	"tofu/internal/konst"
	"tofu/internal/tools/code/debug"
	"tofu/internal/tools/contract"
)

func debugTool(dir string) (contract.Tool, error) {
	return debug.New(dir, debug.Limits{
		Request:     konst.DebugRequestMillis * time.Millisecond,
		Run:         konst.DebugRunMillis * time.Millisecond,
		Stop:        konst.DebugStopMillis * time.Millisecond,
		Idle:        konst.DebugIdleMillis * time.Millisecond,
		Frames:      konst.DebugStackFrames,
		Locals:      konst.DebugLocals,
		ValueRunes:  konst.DebugValueRunes,
		OutputLines: konst.DebugOutputLines,
	})
}
