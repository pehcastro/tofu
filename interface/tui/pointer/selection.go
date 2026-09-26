package pointer

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/look"
	"tofu/internal/sys"
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
	eachSpan(lines, s, pane, func(y, left, right int) {
		selected = append(selected, strings.TrimRight(ansi.Strip(ansi.Cut(lines[y], left, right)), " "))
	})
	return strings.Join(selected, "\n")
}

func Paint(frame string, s Selection, pane Pane) string {
	if !s.Dragged {
		return frame
	}
	lines := strings.Split(frame, "\n")
	eachSpan(lines, s, pane, func(y, left, right int) {
		line := lines[y]
		lines[y] = ansi.Cut(line, 0, left) + look.Painted(ansi.Strip(ansi.Cut(line, left, right)), look.Background, look.MutedColor) + ansi.Cut(line, right, pane.Width)
	})
	return strings.Join(lines, "\n")
}

func eachSpan(lines []string, s Selection, pane Pane, visit func(y, left, right int)) {
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
		contentEnd := pane.Width
		if IsTrackGlyph(lines[y], pane.Width-1) {
			contentEnd--
		}
		right = min(right, ansi.StringWidth(strings.TrimRight(ansi.Strip(ansi.Cut(lines[y], 0, contentEnd)), " ")))
		if left = max(0, left); left < right {
			visit(y, left, right)
		}
	}
}

type Copied struct {
	Text string
	Err  error
}

func Copy(text string) tea.Cmd {
	return func() tea.Msg {
		return Copied{Text: text, Err: sys.WriteClipboardText(text)}
	}
}
