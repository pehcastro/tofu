package progress

import (
	"time"

	"tofu/interface/tui/theme"
	"tofu/internal/widget"
)

const (
	Frames       = "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"
	finishedMark = "· "
	TickInterval = 250 * time.Millisecond
)

type Line struct {
	Label string
	Frame int
	Live  bool
}

func (l Line) View(width int) string {
	mark, style := finishedMark, theme.Added()
	if l.Live {
		mark, style = Spin(l.Frame)+" ", theme.Accent()
	}
	return style.Render(widget.Fit(mark+l.Label, width))
}

func Spin(frame int) string {
	frames := []rune(Frames)
	return string(frames[frame%len(frames)])
}
