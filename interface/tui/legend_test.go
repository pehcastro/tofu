package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"tofu/internal/golden"
)

const riskSentence = "risk is hard to undo or reaches outside the workspace, so the call is asked about"

func TestAGateAskDrawsTheWordTheLegendGaveTheScore(t *testing.T) {
	app := awaitingApp(t, make(chan Answer, 1))
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	content := app.View().Content
	plain := ansi.Strip(content)
	if !strings.Contains(plain, riskSentence) {
		t.Fatalf("the ask does not say what the score means\n%s", plain)
	}
	if !strings.Contains(plain, "risk            2.00") {
		t.Fatalf("the score itself left the screen\n%s", plain)
	}
	if strings.Contains(plain, "risk_ask_at") {
		t.Fatalf("the threshold line still names the constant\n%s", plain)
	}
	golden.Assert(t, "session-awaiting-120x36.golden", content)
}
