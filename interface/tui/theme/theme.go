package theme

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

const (
	ink        = "252"
	speech     = "255"
	muted      = "245"
	faint      = "240"
	accentBlue = "39"
	toolCyan   = "44"
	callPurple = "141"
	pathBlue   = "110"
	idTan      = "223"
	warnAmber  = "214"
	failRed    = "203"
	addGreen   = "114"
	dropRed    = "167"
	panel      = "236"
	composer   = "8"
	selected   = "24"
)

func Text() lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(ink)) }

func Speech() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(speech)).Bold(true)
}

func Dim() lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(muted)) }

func Faint() lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(faint)) }

func Accent() lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(accentBlue)) }

func Tool() lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(toolCyan)) }

func Call() lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(callPurple)) }

func Path() lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(pathBlue)) }

func ID() lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(idTan)) }

func Warn() lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(warnAmber)) }

func Fail() lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(failRed)) }

func Added() lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(addGreen)) }

func Removed() lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(dropRed)) }

func Bar() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(ink)).
		Background(lipgloss.Color(panel))
}

func Selected() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(speech)).
		Background(lipgloss.Color(selected))
}

func Rule() lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(panel)) }

func ComposerColor() color.Color { return lipgloss.Color(composer) }
