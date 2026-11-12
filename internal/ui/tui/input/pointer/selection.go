package pointer

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"tofu/internal/ui/tui/look/look"
)

type Selection struct {
	Start, End       Cell
	Pressed, Dragged bool
	Mods             Mods
}

func (s Selection) Bounds() (Cell, Cell) {
	a, b := s.Start, s.End
	if a.Y > b.Y || a.Y == b.Y && a.X > b.X {
		a, b = b, a
	}
	return a, b
}

type Pane struct{ Split, Width, Top, Bottom int }

func SelectedText(frame string, s Selection, pane Pane) string {
	if !s.Dragged {
		return ""
	}
	lines := strings.Split(frame, "\n")
	var selected []string
	eachSpan(lines, s, pane, func(_, left, right int, plain string) {
		selected = append(selected, strings.TrimRight(ansi.Cut(plain, left, right), " "))
	})
	return strings.Join(selected, "\n")
}

func Paint(frame string, s Selection, pane Pane) string {
	if !s.Dragged {
		return frame
	}
	lines := strings.Split(frame, "\n")
	eachSpan(lines, s, pane, func(y, left, right int, plain string) {
		line := lines[y]
		lines[y] = ansi.Cut(line, 0, left) + look.Painted(ansi.Cut(plain, left, right), look.Background, look.MutedColor) + ansi.Cut(line, right, pane.Width)
	})
	return strings.Join(lines, "\n")
}

func eachSpan(lines []string, s Selection, pane Pane, visit func(y, left, right int, plain string)) {
	start, end := s.Bounds()
	for y := max(0, start.Y); y <= end.Y && y < len(lines); y++ {
		left, right := 0, pane.Width
		if y == start.Y {
			left = start.X
		}
		if y == end.Y {
			right = end.X + 1
		}
		if pane.Split > 0 && s.Start.Y >= pane.Top && s.Start.Y <= pane.Bottom {
			if s.Start.X < pane.Split {
				right = min(right, pane.Split)
			} else {
				left = max(left, pane.Split)
			}
		}
		plain := ansi.Strip(lines[y])
		if left, right = max(0, left), min(right, contentWidth(plain, pane.Width)); left < right {
			visit(y, left, right, plain)
		}
	}
}

func contentWidth(plain string, width int) int {
	content := strings.TrimRight(plain, " ")
	cells := ansi.StringWidth(content)
	for _, glyph := range []string{"┃", "│"} {
		if beforeTrack, onTrack := strings.CutSuffix(content, glyph); onTrack && cells == width {
			return width - 1 - (len(beforeTrack) - len(strings.TrimRight(beforeTrack, " ")))
		}
	}
	return cells
}
