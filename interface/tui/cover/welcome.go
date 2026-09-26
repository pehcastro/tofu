package cover

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	minInnerWidth = 28
	sideMargin    = 2
	maxInputWidth = 72
)

func WelcomeInputView(identity Identity, width, height int, input string, frame int, hint string) string {
	inner := max(minInnerWidth, width-2*sideMargin)
	stage := min(stageWidth, inner)
	var identityView string
	if frame < 0 {
		identityView = identity.View(stage)
	} else {
		identityView = identity.ViewFrame(stage, frame)
	}
	help := lipgloss.NewStyle().Foreground(lipgloss.Color(hintColor)).Width(stage).Align(lipgloss.Center).Render(hint)
	prompt := WelcomeInput(input, stage)
	content := prompt
	for _, candidate := range []string{
		identityView + "\n" + prompt + "\n" + help,
		compactIdentity(identityView) + "\n" + prompt + "\n" + help,
		prompt + "\n" + help,
	} {
		if lipgloss.Height(candidate) <= height {
			content = candidate
			break
		}
	}
	return lipgloss.NewStyle().Padding(0, sideMargin).Render(
		lipgloss.Place(inner, max(1, height), lipgloss.Center, lipgloss.Center, content),
	)
}

func WelcomeInput(input string, width int) string {
	return lipgloss.NewStyle().
		Width(min(maxInputWidth, width)-2).
		Padding(0, 1).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(borderColor)).
		Render(lipgloss.NewStyle().Foreground(lipgloss.Color(Mint)).Render("› ") + input)
}

func compactIdentity(view string) string {
	var rows []string
	for _, row := range strings.Split(view, "\n") {
		if strings.TrimSpace(ansi.Strip(row)) != "" || strings.Contains(row, "\x1b[48;") {
			rows = append(rows, row)
		}
	}
	return strings.Join(rows, "\n")
}
