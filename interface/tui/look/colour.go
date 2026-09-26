package look

import (
	"image/color"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type Color string

const (
	Background           Color = "#181620"
	Panel                Color = "#23222c"
	PanelLight           Color = "#302e3a"
	Text                 Color = "#d4d7df"
	MutedColor           Color = "#7f8493"
	FaintColor           Color = "#4d515d"
	Mint                 Color = "#40d294"
	Blue                 Color = "#8fb7cc"
	Amber                Color = "#d4ad78"
	Red                  Color = "#d38b92"
	Violet               Color = "#b5a1d2"
	ReferenceType        Color = "#a999c0"
	ReferenceID          Color = "#c2add9"
	SyntaxKeyword        Color = "#c49acb"
	SyntaxFunction       Color = "#9dbfd1"
	SyntaxString         Color = "#d5c58d"
	SyntaxComment        Color = "#71808a"
	SyntaxNumber         Color = "#9fc8cb"
	DiffAddBackground    Color = "#20362d"
	DiffDeleteBackground Color = "#392723"
)

const (
	sgrForeground = 38
	sgrBackground = 48
	hexShape      = "#rrggbb"
)

func (c Color) value() color.Color { return lipgloss.Color(string(c)) }

func Style(c Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c.value()) }

func Painted(text string, fg, bg Color) string {
	if !singleRun(text, fg, bg) {
		return Style(fg).Background(bg.value()).Render(text)
	}
	return "\x1b[" + sgr(sgrForeground, fg) + ";" + sgr(sgrBackground, bg) + "m" + text + ansi.ResetStyle
}

func coloured(text string, fg Color) string {
	if !singleRun(text, fg) {
		return Style(fg).Render(text)
	}
	return "\x1b[" + sgr(sgrForeground, fg) + "m" + text + ansi.ResetStyle
}

func singleRun(text string, colours ...Color) bool {
	for _, c := range colours {
		if len(c) != len(hexShape) || c[0] != hexShape[0] {
			return false
		}
	}
	return !strings.ContainsAny(text, "\t\n")
}

func Title(text string) string  { return Style(Text).Bold(true).Render(text) }
func Muted(text string) string  { return coloured(text, MutedColor) }
func Faint(text string) string  { return coloured(text, FaintColor) }
func Accent(text string) string { return coloured(text, Mint) }

func PaneTitle(text string, focused bool) string {
	if focused {
		return Style(Mint).Bold(true).Render(text)
	}
	return Title(text)
}

func SectionLabel(label string) string {
	return Style(Amber).Bold(true).Render(strings.ToUpper(label))
}

func Sides(left, right string, width int) string {
	room := width - lipgloss.Width(left) - lipgloss.Width(right)
	if room < 1 {
		return left
	}
	return left + strings.Repeat(" ", room) + right
}

func ChromeRow(width int, bg Color, left, right string) string {
	if lipgloss.Width(left) > width {
		left = ansi.Truncate(left, width, "")
	}
	room := width - lipgloss.Width(left) - lipgloss.Width(right)
	if room < 1 {
		right = ""
		room = max(0, width-lipgloss.Width(left))
	}
	return left + Painted(strings.Repeat(" ", room), Text, bg) + right
}

func FixedBlock(width, height int, content string) string {
	width, height = max(1, width), max(1, height)
	return lipgloss.NewStyle().Width(width).MaxWidth(width).Height(height).MaxHeight(height).Render(content)
}

func ComposerStyles() textarea.Styles {
	panel := lipgloss.NewStyle().Background(PanelLight.value())
	styles := textarea.DefaultDarkStyles()
	styles.Focused.Base, styles.Focused.CursorLine, styles.Focused.EndOfBuffer = panel, panel, panel
	styles.Focused.Text = Style(Text)
	styles.Focused.Placeholder = panel.Foreground(MutedColor.value())
	styles.Focused.Prompt = panel.Foreground(Mint.value())
	styles.Blurred.Base, styles.Blurred.CursorLine, styles.Blurred.EndOfBuffer = panel, panel, panel
	styles.Blurred.Text = Style(MutedColor)
	styles.Blurred.Placeholder = panel.Foreground(FaintColor.value())
	styles.Blurred.Prompt = panel.Foreground(FaintColor.value())
	styles.Cursor.Color = Blue.value()
	return styles
}

func FilterStyles() textinput.Styles {
	styles := textinput.DefaultDarkStyles()
	styles.Focused.Text = Style(Text)
	styles.Focused.Placeholder = Style(MutedColor)
	styles.Focused.Prompt = Style(Mint)
	styles.Cursor.Color = Mint.value()
	return styles
}

func (c Color) rgb() color.RGBA {
	value, _ := strconv.ParseUint(strings.TrimPrefix(string(c), "#"), 16, 32)
	return color.RGBA{R: uint8(value >> 16), G: uint8(value >> 8), B: uint8(value), A: 255}
}

func sgr(mode int, c Color) string {
	rgb := c.rgb()
	return strconv.Itoa(mode) + ";2;" + strconv.Itoa(int(rgb.R)) + ";" + strconv.Itoa(int(rgb.G)) + ";" + strconv.Itoa(int(rgb.B))
}
