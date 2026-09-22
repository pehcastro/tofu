package settings

import (
	"testing"

	"tofu/interface/tui/frametime"
)

func TestTheSettingsViewRendersInsideTheFrameBudget(t *testing.T) {
	rows := baseRows()
	rows[1].Value, rows[1].Changed, rows[1].RestartPending, rows[1].Source = "12", true, true, "silo/.tofu/settings.json"
	m := Model{
		Providers: []Provider{
			{Name: "claude-sub", State: "signed in"},
			{Name: "openrouter", Key: "sk-or-v1-77c1f0b6e5a94d2f8badc0ffee1234567890abcd", State: "ok"},
			{Name: "jev", State: "build jev-2026-09-01"},
			{Name: "codex-sub", Fix: "tofu login codex-sub"},
		},
		Scopes: []string{"global", "project"},
		Scope:  1,
		Rows:   rows,
		Query:  "decision",
	}
	m.SetSize(120, 36)
	frametime.Frames(t, "the settings view open at 120x36", func() { m.View() })
}
