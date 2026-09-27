package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/session"
)

func promoteApp(t *testing.T, at time.Time) *App {
	t.Helper()
	app := newTestApp(Options{Repo: testRepo, Branch: "develop", Now: func() time.Time { return at }, Wires: anthropicAlone})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	app.Update(Event{Kind: EventToolCall, ID: "r1", Tool: "read", Text: "a.go"})
	app.Update(Event{Kind: EventToolResult, ID: "r1", Text: "ok"})
	return app
}

func TestASpawnIsNeverAToolRowInChat(t *testing.T) {
	for _, ending := range []struct {
		name  string
		event Event
	}{
		{"finished", Event{Kind: EventToolResult, ID: "c1", Text: "wrote docs/verification.md"}},
		{"asked", Event{Kind: EventDecision, Promote: true, Decision: &session.Decision{Tool: "subagent", Verdict: session.Ask}}},
		{"failed", Event{Kind: EventToolResult, ID: "c1", Text: "the child errored", Failed: true}},
	} {
		app := promoteApp(t, time.Now())
		app.Update(Event{Kind: EventToolCall, ID: "c1", Tool: "subagent", Text: "go-docs: write the docs", Promote: true})
		app.Update(ending.event)
		if plain := ansi.Strip(app.View().Content); strings.Contains(plain, "subagent go-docs") {
			t.Errorf("a %s spawn was drawn as a tool row in chat\n%s", ending.name, plain)
		}
	}
}

func TestAPlainToolCallDoesNotPromote(t *testing.T) {
	app := promoteApp(t, time.Now())
	plain := ansi.Strip(app.View().Content)
	if strings.Contains(plain, "⟩ read a.go") {
		t.Fatalf("a plain tool call promoted on its own without the switch\n%s", plain)
	}
	if !strings.Contains(plain, "(1) tools") {
		t.Fatalf("a plain tool call did not fold\n%s", plain)
	}
}
