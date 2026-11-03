package progress

import (
	"time"

	"tofu/internal/konst"
)

const (
	Frames       = "⠋⠙⠚⠞⠖⠋⠉⠈"
	WorkFrames   = "⠁⠁⠉⠙⠚⠒⠂⠂⠒⠲⠴⠤⠄⠄⠤⠠⠠⠤⠦⠖⠒⠐⠐⠒⠓⠋⠉⠈⠈"
	TickInterval = konst.ProgressTickMillis * time.Millisecond
)

func Spin(frame int) string { return frameOf(Frames, frame) }

func Work(frame int) string { return frameOf(WorkFrames, frame) }

func frameOf(frames string, frame int) string {
	runes := []rune(frames)
	return string(runes[frame%len(runes)])
}
