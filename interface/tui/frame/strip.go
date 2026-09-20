package frame

import (
	"strings"

	"tofu/interface/tui/theme"
	"tofu/internal/widget"
)

const stripGap = "   "

type View struct {
	Digit rune
	Name  string
}

type zone struct {
	first int
	last  int
}

type Strip struct {
	Views   []View
	Notice  string
	Current int
	zones   []zone
}

func (s *Strip) Render(width int) string {
	s.zones = make([]zone, 0, len(s.Views))
	var out strings.Builder
	column := 0
	for index, view := range s.Views {
		gap := stripGap
		if index == 0 {
			gap = ""
		}
		label := string(view.Digit) + " " + view.Name
		if column+widget.Cells(gap)+widget.Cells(label) > width {
			break
		}
		out.WriteString(gap)
		column += widget.Cells(gap)
		s.zones = append(s.zones, zone{first: column, last: column + widget.Cells(label) - 1})
		style := theme.Faint()
		if index == s.Current {
			style = theme.Accent()
		}
		out.WriteString(style.Render(label))
		column += widget.Cells(label)
	}
	room := width - column
	if room <= 0 {
		return out.String()
	}
	if s.Notice == "" {
		return out.String() + strings.Repeat(" ", room)
	}
	notice := widget.Fit(s.Notice, room-widget.Cells(stripGap))
	return out.String() + theme.Dim().Render(widget.Lead(notice, room))
}

func (s Strip) Hit(column int) (int, bool) {
	for index, zone := range s.zones {
		if column >= zone.first && column <= zone.last {
			return index, true
		}
	}
	return 0, false
}
