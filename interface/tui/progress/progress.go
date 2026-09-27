package progress

import (
	"time"

	"tofu/interface/tui/theme"
	"tofu/internal/konst"
	"tofu/internal/widget"
)

const (
	Frames       = "⠋⠙⠚⠞⠖⠋⠉⠈"
	WorkFrames   = "⠁⠁⠉⠙⠚⠒⠂⠂⠒⠲⠴⠤⠄⠄⠤⠠⠠⠤⠦⠖⠒⠐⠐⠒⠓⠋⠉⠈⠈"
	finishedMark = "· "
	TickInterval = konst.ProgressTickMillis * time.Millisecond
)

type Line struct {
	Label string
	Frame int
	Live  bool
}

func (l Line) View(width int) string {
	mark, style := finishedMark, theme.Added()
	if l.Live {
		mark, style = Work(l.Frame)+" ", theme.Accent()
	}
	return style.Render(widget.Fit(mark+l.Label, width))
}

func Spin(frame int) string { return frameOf(Frames, frame) }

func Work(frame int) string { return frameOf(WorkFrames, frame) }

func frameOf(frames string, frame int) string {
	runes := []rune(frames)
	return string(runes[frame%len(runes)])
}
