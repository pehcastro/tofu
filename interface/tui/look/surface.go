package look

import (
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	dialogMinInner = 16
	dialogPadX     = 2
	selectedMarker = "› "
	idleMarker     = "  "
)

func Surface(width, height int, bg Color, padding int, content string) string {
	width, height = max(1, width), max(1, height)
	return lipgloss.NewStyle().
		Width(width).MaxWidth(width).
		Height(height).MaxHeight(height).
		PaddingLeft(padding).PaddingRight(padding).
		Background(bg.value()).
		Render(KeepSurfaceBackground(content, bg))
}

func TintedSurface(width int, bg Color, content string) string {
	return lipgloss.NewStyle().Width(width).Padding(1, dialogPadX).Background(bg.value()).Render(KeepSurfaceBackground(content, bg))
}

func DialogPanel(width int, heading, description, body, footer string) string {
	inner := max(dialogMinInner, width-2*dialogPadX)
	content := Sides(Title(heading), Faint("esc"), inner) + "\n" + Muted(description) + "\n\n" + body + "\n\n" + Faint(footer)
	return TintedSurface(width, Panel, content)
}

func ModalPane(width, height int, bg Color, horizontalPadding int, content string) string {
	return lipgloss.NewStyle().Width(max(1, width)).Height(max(1, height)).Padding(1, horizontalPadding).Background(bg.value()).Render(KeepSurfaceBackground(content, bg))
}

func DialogChoice(selected bool, label string) string {
	if selected {
		return Accent("> ") + Style(Text).Background(PanelLight.value()).Padding(0, 1).Render(label)
	}
	return "  " + Muted(label)
}

func ModalRow(width int, selected bool, label, description, key string) string {
	return Sides(DialogChoice(selected, label), Faint(key), width) + "\n  " + Muted(description)
}

func VisibleRows(count, cursor, limit int) (start, end int) {
	if count <= limit {
		return 0, count
	}
	start = max(0, min(cursor-limit/2, count-limit))
	return start, start + limit
}

func CatalogRow(width int, selected bool, label string) string {
	if selected {
		return Accent(selectedMarker) + Title(label)
	}
	return Faint(idleMarker) + Muted(label)
}

func Over(base, dialog string, x, y int) string {
	rows, layer := strings.Split(base, "\n"), strings.Split(dialog, "\n")
	for len(rows) < y+len(layer) {
		rows = append(rows, "")
	}
	widths := make([]int, len(layer))
	for i, line := range layer {
		widths[i] = ansi.StringWidth(line)
	}
	end := x + slices.Max(widths)
	for i, line := range layer {
		row := rows[y+i]
		left, right := ansi.Truncate(row, x, ""), ""
		if beyond := ansi.StringWidth(row) - end; beyond > 0 {
			right = ansi.TruncateLeft(row, end, "")
			if ansi.StringWidth(right) > beyond {
				right = " " + ansi.TruncateLeft(row, end+1, "")
			}
		}
		rows[y+i] = closed(left) + strings.Repeat(" ", x-ansi.StringWidth(left)) + closed(line) + strings.Repeat(" ", end-x-widths[i]) + right
	}
	for i, row := range rows {
		rows[i] = closed(strings.TrimRight(row, " "))
	}
	return strings.Join(rows, "\n")
}

func closed(styled string) string {
	if !strings.Contains(styled, "\x1b") || strings.HasSuffix(styled, ansi.ResetStyle) {
		return styled
	}
	return styled + ansi.ResetStyle
}

func KeepSurfaceBackground(content string, bg Color) string {
	if bg == "" {
		return content
	}
	resume := "\x1b[" + sgr(sgrBackground, bg) + "m"
	content = strings.ReplaceAll(content, "\x1b[m", "\x1b[m"+resume)
	return strings.ReplaceAll(content, "\x1b[49m", resume)
}

func JoinFixedPanes(left, right string) string {
	leftRows, rightRows := strings.Split(left, "\n"), strings.Split(right, "\n")
	if len(leftRows) != len(rightRows) {
		return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	}
	var joined strings.Builder
	joined.Grow(len(left) + len(right))
	for row := range leftRows {
		if row > 0 {
			joined.WriteByte('\n')
		}
		joined.WriteString(leftRows[row])
		joined.WriteString(rightRows[row])
	}
	return joined.String()
}

type PaneCache struct {
	width, height, padding int
	bg                     Color
	content, view          string
}

func (c *PaneCache) Surface(width, height int, bg Color, padding int, content string) string {
	if c.view != "" && c.width == width && c.height == height && c.bg == bg && c.padding == padding && c.content == content {
		return c.view
	}
	view, fast := "", false
	if bg == "" {
		view, fast = PlainSurfaceFast(width, height, padding, content)
	}
	if !fast {
		view = Surface(width, height, bg, padding, content)
	}
	*c = PaneCache{width: width, height: height, padding: padding, bg: bg, content: content, view: view}
	return view
}

func PlainSurfaceFast(width, height, padding int, content string) (string, bool) {
	inner := width - 2*padding
	if width <= 0 || height <= 0 || inner < 1 || !plainControls(content) {
		return "", false
	}
	lines := strings.Split(content, "\n")
	for _, line := range lines[:min(len(lines), height)] {
		if ansi.StringWidth(line) > inner {
			lines = strings.Split(lipgloss.Wrap(content, inner, ""), "\n")
			break
		}
	}
	lines = lines[:min(len(lines), height)]
	widths := make([]int, len(lines))
	for i, line := range lines {
		widths[i] = ansi.StringWidth(line)
		if widths[i] > inner {
			return "", false
		}
	}
	left := strings.Repeat(" ", padding)
	blank := strings.Repeat(" ", width)
	var view strings.Builder
	view.Grow(height*(width+1) + len(content))
	for row := range height {
		if row > 0 {
			view.WriteByte('\n')
		}
		if row >= len(lines) {
			view.WriteString(blank)
			continue
		}
		view.WriteString(left)
		view.WriteString(lines[row])
		view.WriteString(strings.Repeat(" ", width-padding-widths[row]))
	}
	return view.String(), true
}

func plainControls(content string) bool {
	for i := 0; i < len(content); i++ {
		switch b := content[i]; {
		case b == '\n':
		case b == '\x1b':
			if i+1 >= len(content) || content[i+1] != '[' {
				return false
			}
			i += 2
			for i < len(content) && (content[i] >= '0' && content[i] <= '9' || content[i] == ';' || content[i] == ':') {
				i++
			}
			if i >= len(content) || content[i] != 'm' {
				return false
			}
		case b < ' ' || b == '\x7f':
			return false
		}
	}
	return true
}
