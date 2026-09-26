package theme

import (
	"image/color"

	"charm.land/lipgloss/v2"

	"tofu/interface/tui/look"
)

func paint(c look.Color) color.Color { return lipgloss.Color(string(c)) }

func ink(c look.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(paint(c)) }

func Text() lipgloss.Style { return ink(look.Text) }

func Speech() lipgloss.Style { return ink(look.Text).Bold(true) }

func Dim() lipgloss.Style { return ink(look.MutedColor) }

func Faint() lipgloss.Style { return ink(look.FaintColor) }

func Accent() lipgloss.Style { return ink(look.Mint) }

func Tool() lipgloss.Style { return ink(look.Amber) }

func Call() lipgloss.Style { return ink(look.Blue) }

func Path() lipgloss.Style { return ink(look.Blue) }

func ID() lipgloss.Style { return ink(look.ReferenceID) }

func Warn() lipgloss.Style { return ink(look.Amber) }

func Fail() lipgloss.Style { return ink(look.Red) }

func Added() lipgloss.Style { return ink(look.Mint) }

func Removed() lipgloss.Style { return ink(look.Red) }

func Bar() lipgloss.Style { return ink(look.Text).Background(paint(look.Panel)) }

func Selected() lipgloss.Style { return ink(look.Text).Background(paint(look.PanelLight)) }

func Rule() lipgloss.Style { return ink(look.FaintColor) }

func ComposerColor() color.Color { return paint(look.PanelLight) }
