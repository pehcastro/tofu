package pane

import (
	"strings"

	"charm.land/lipgloss/v2"

	"tofu/internal/widget"
)

func Cell(text string, width int, style lipgloss.Style) string {
	return style.Render(widget.Pad(widget.Fit(text, width), width))
}

func Block(marker, body string, width int, style lipgloss.Style) []string {
	room := max(width-widget.Cells(marker), 1)
	var lines []string
	for index, line := range widget.Wrap(body, room) {
		prefix := marker
		if index > 0 {
			prefix = strings.Repeat(" ", widget.Cells(marker))
		}
		lines = append(lines, Cell(prefix+line, width, style))
	}
	return lines
}

func Fill(lines []string, height, width int) []string {
	for len(lines) < height {
		lines = append(lines, strings.Repeat(" ", width))
	}
	return lines[:height]
}
