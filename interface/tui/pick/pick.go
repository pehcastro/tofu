package pick

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/theme"
	"tofu/internal/widget"
)

const (
	drawn      = "»▌⟩·!├└─│▏⟳"
	lineStarts = drawn + "#"
)

type Cell struct{ X, Y int }

type Selection struct {
	from Cell
	to   Cell
	on   bool
}

func (s *Selection) Begin(at Cell) { s.from, s.to, s.on = at, at, true }

func (s *Selection) Extend(at Cell) {
	if s.on {
		s.to = at
	}
}

func (s *Selection) Clear() { *s = Selection{} }

func (s Selection) On() bool { return s.on }

func (s *Selection) Shift(rows int) {
	s.from.Y += rows
	s.to.Y += rows
}

func (s Selection) ordered() (Cell, Cell) {
	if s.to.Y < s.from.Y || (s.to.Y == s.from.Y && s.to.X < s.from.X) {
		return s.to, s.from
	}
	return s.from, s.to
}

func (s Selection) covers(row int, plain string) (int, int) {
	head, tail := s.ordered()
	first, last := 0, widget.Cells(plain)-1
	if row == head.Y {
		first = max(head.X, 0)
	}
	if row == tail.Y {
		last = min(last, tail.X)
	}
	return first, last
}

func (s Selection) Paint(rows []string) []string {
	if !s.on {
		return rows
	}
	head, tail := s.ordered()
	painted := append([]string{}, rows...)
	for row := max(head.Y, 0); row <= tail.Y && row < len(painted); row++ {
		plain := ansi.Strip(painted[row])
		first, last := s.covers(row, plain)
		if first > last {
			continue
		}
		painted[row] = ansi.Truncate(painted[row], first, "") +
			theme.Selected().Render(cut(plain, first, last)) +
			ansi.TruncateLeft(painted[row], last+1, "")
	}
	return painted
}

func (s Selection) Text(rows []string, width int) string {
	if !s.on {
		return ""
	}
	head, tail := s.ordered()
	var taken, whole []string
	for row := max(head.Y, 0); row <= tail.Y && row < len(rows); row++ {
		plain := strings.TrimRight(ansi.Strip(rows[row]), " ")
		first, last := s.covers(row, plain)
		taken = append(taken, undrawn(cut(plain, first, last)))
		whole = append(whole, plain)
	}
	var out strings.Builder
	for index, line := range taken {
		switch {
		case index == 0:
		case wraps(whole[index-1], whole[index], width):
			out.WriteString(" ")
			line = strings.TrimLeft(line, " ")
		default:
			out.WriteString("\n")
		}
		out.WriteString(line)
	}
	return strings.Trim(out.String(), " \n")
}

func wraps(previous, next string, width int) bool {
	if previous == "" || !strings.HasPrefix(next, " ") {
		return false
	}
	word, _, _ := strings.Cut(strings.TrimLeft(next, " "), " ")
	opener, _ := utf8.DecodeRuneInString(word)
	if word == "" || strings.ContainsRune(lineStarts, opener) {
		return false
	}
	return widget.Cells(previous)+1+widget.Cells(word) > width
}

func undrawn(line string) string {
	bare := strings.TrimLeft(line, " ")
	stripped := strings.TrimLeft(bare, drawn+" ")
	if stripped == bare {
		return line
	}
	return stripped
}

func cut(plain string, first, last int) string {
	if last < first {
		return ""
	}
	return ansi.Cut(plain, first, last+1)
}
