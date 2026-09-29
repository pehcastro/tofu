package theme

import (
	"charm.land/lipgloss/v2"

	"tofu/interface/tui/look"
)

func ink(c look.Color) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(string(c)))
}

func Faint() lipgloss.Style { return ink(look.FaintColor) }

func Accent() lipgloss.Style { return ink(look.Mint) }

func Tool() lipgloss.Style { return ink(look.Amber) }

func Fail() lipgloss.Style { return ink(look.Red) }

func Added() lipgloss.Style { return ink(look.Mint) }
