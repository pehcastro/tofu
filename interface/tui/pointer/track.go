package pointer

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/look"
)

const minTrackHeight = 4

type Track struct{ Top, Height, Total, Visible, FromTop int }

func (t Track) scrolls() bool { return t.Total > t.Visible && t.Height >= minTrackHeight }

func Overlay(frame string, width int, track Track) string {
	if !track.scrolls() {
		return frame
	}
	lines := strings.Split(frame, "\n")
	for i, glyph := range strings.Split(look.ScrollTrack(track.Height, track.Total, track.Visible, track.FromTop), "\n") {
		row := track.Top + i
		if row >= len(lines) {
			break
		}
		prefix := ansi.Cut(lines[row], 0, width-1)
		lines[row] = prefix + strings.Repeat(" ", max(0, width-1-ansi.StringWidth(prefix))) + glyph
	}
	return strings.Join(lines, "\n")
}

func IsTrackGlyph(line string, x int) bool {
	glyph := ansi.Strip(ansi.Cut(line, x, x+1))
	return glyph == "┃" || glyph == "│"
}

type Drag struct {
	Active                        bool
	Top, Height, Thumb, Grab, Max int
}

func Press(x, y, width int, track Track) (Drag, bool) {
	if x != width-1 || y < track.Top || y >= track.Top+track.Height || !track.scrolls() {
		return Drag{}, false
	}
	thumb := max(1, min(track.Height, track.Height*track.Visible/track.Total))
	travel := track.Height - thumb
	thumbTop := track.Top + max(0, min(travel, track.FromTop*travel/(track.Total-track.Visible)))
	grab := thumb / 2
	if y >= thumbTop && y < thumbTop+thumb {
		grab = y - thumbTop
	}
	return Drag{Active: true, Top: track.Top, Height: track.Height, Thumb: thumb, Grab: grab, Max: track.Total - track.Visible}, true
}

func (d Drag) Scroll(y int) int {
	travel := max(1, d.Height-d.Thumb)
	position := max(0, min(travel, y-d.Top-d.Grab))
	return d.Max - position*d.Max/travel
}
