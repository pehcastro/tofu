package theme

import (
	"charm.land/lipgloss/v2"

	"tofu/internal/ui/tui/look/look"
)

func ink(c look.Color) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(string(c)))
}

func Faint() lipgloss.Style { return ink(look.FaintColor) }

func Tool() lipgloss.Style { return ink(look.Amber) }

func Fail() lipgloss.Style { return ink(look.Red) }
