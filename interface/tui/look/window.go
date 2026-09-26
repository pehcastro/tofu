package look

import (
	"strings"

	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const trackWidth = 1

func Window(content string, width, height, offset int) (view string, total, fromTop int) {
	width, height = max(1+trackWidth, width), max(1, height)
	inner := width - trackWidth
	if strings.ContainsRune(content, '\r') {
		pager := viewport.New(viewport.WithWidth(inner), viewport.WithHeight(height))
		pager.SetContent(content)
		pager.GotoBottom()
		pager.ScrollUp(max(0, offset))
		return pager.View(), pager.TotalLineCount(), pager.YOffset()
	}
	lines := strings.Split(content, "\n")
	if len(lines) == 1 && ansi.StringWidth(lines[0]) == 0 {
		lines = nil
	}
	total = len(lines)
	fromTop = max(0, total-height-min(max(0, offset), max(0, total-height)))
	visible := lines[fromTop:min(total, fromTop+height)]
	copied := false
	for i, line := range visible {
		if ansi.StringWidth(line) <= inner {
			continue
		}
		if !copied {
			visible, copied = append([]string(nil), visible...), true
		}
		visible[i] = ansi.Cut(line, 0, inner)
	}
	return lipgloss.NewStyle().Width(inner).Height(height).Render(strings.Join(visible, "\n")), total, fromTop
}

func WindowTracked(content string, width, height, offset int) string {
	view, total, fromTop := Window(content, width, height, offset)
	height = max(1, height)
	return JoinScrollTrack(view, ScrollTrack(height, total, height, fromTop))
}

func ScrollTrack(height, total, visible, fromTop int) string {
	if total <= visible || height < 2 {
		return strings.Repeat(" \n", max(1, height)-1) + " "
	}
	thumb := max(1, min(height, height*visible/total))
	travel := height - thumb
	position := max(0, min(travel, fromTop*travel/(total-visible)))
	rows := make([]string, height)
	for row := range rows {
		if row >= position && row < position+thumb {
			rows[row] = Style(Blue).Render("┃")
		} else {
			rows[row] = Faint("│")
		}
	}
	return strings.Join(rows, "\n")
}

func JoinScrollTrack(view, track string) string {
	left, right := strings.Split(view, "\n"), strings.Split(track, "\n")
	pad := strings.Repeat(" ", ansi.StringWidth(right[0]))
	var out strings.Builder
	out.Grow(len(view) + len(track))
	for i, row := range left {
		if i > 0 {
			out.WriteByte('\n')
		}
		out.WriteString(row)
		if i < len(right) {
			out.WriteString(right[i])
		} else {
			out.WriteString(pad)
		}
	}
	return out.String()
}
