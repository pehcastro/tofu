package tui

import (
	"strings"

	"charm.land/lipgloss/v2"

	"tofu/internal/ui/tui/look/look"
)

func escapeOf(style lipgloss.Style) string {
	before, _, _ := strings.Cut(look.Apply(style.Render("X"), look.ThemeTofu), "X")
	return before[strings.LastIndex(before, "\x1b["):]
}
