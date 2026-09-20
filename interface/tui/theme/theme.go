package theme

import "charm.land/lipgloss/v2"

const (
	ink        = "252"
	speech     = "255"
	muted      = "245"
	faint      = "240"
	accentBlue = "39"
	toolCyan   = "44"
	warnAmber  = "214"
	failRed    = "203"
	addGreen   = "114"
	dropRed    = "167"
	panel      = "236"
)

func Text() lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(ink)) }

func Speech() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(speech)).Bold(true)
}

func Dim() lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(muted)) }

func Faint() lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(faint)) }

func Accent() lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(accentBlue)) }

func Tool() lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(toolCyan)) }

func Warn() lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(warnAmber)) }

func Fail() lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(failRed)) }

func Added() lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(addGreen)) }

func Removed() lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(dropRed)) }

func Bar() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(ink)).
		Background(lipgloss.Color(panel))
}

func Rule() lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(panel)) }
