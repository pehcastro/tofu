package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"tofu/interface/tui/session"
	"tofu/interface/tui/theme"
)

func escapeOf(style lipgloss.Style) string {
	rendered := style.Render("X")
	escape, _, _ := strings.Cut(rendered, "X")
	return escape
}

func TestFiveKindsOfThingEachCarryTheirOwnColour(t *testing.T) {
	app := pathApp(t, make(chan string, 1))
	app.settings.ChatShowsTools = true
	app.view.Append(session.Entry{Kind: session.Tool, ID: "a1a1a1", Head: "read", Body: "interface/tui/session.go", Status: "ok"})
	app.view.Append(session.Entry{Kind: session.Tool, ID: "b2b2b2", Head: "bash", Body: "go test ./...", Status: "ok"})
	typeText(app, "@int")
	frame := app.View().Content

	colours := map[string]string{
		"tool call":     escapeOf(theme.Call()),
		"shell command": escapeOf(theme.Tool()),
		"file path":     escapeOf(theme.Path()),
		"id":            escapeOf(theme.ID()),
		"finished call": escapeOf(theme.Added()),
	}
	for name, escape := range colours {
		if !strings.Contains(frame, escape) {
			t.Errorf("%s carries no colour of its own\n%s", name, frame)
		}
	}
	for left, leftEscape := range colours {
		for right, rightEscape := range colours {
			if left != right && leftEscape == rightEscape {
				t.Fatalf("%s and %s share the same colour", left, right)
			}
		}
	}
}
