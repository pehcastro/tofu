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
	Since time.Duration
	Tick  time.Duration
	Live  bool
}

func (l Line) View(width int) string {
	mark, style := finishedMark, theme.Added()
	if l.Live {
		mark, style = Spin(l.Since, l.Tick)+" ", theme.Accent()
	}
	return style.Render(widget.Fit(mark+l.Label, width))
}

func Spin(since, tick time.Duration) string {
	frames := []rune(Frames)
	return string(frames[int(since/tick)%len(frames)])
}
