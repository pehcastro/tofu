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

func TestAFinishedChildPromotesToChatOnTheSwitchAlone(t *testing.T) {
	app := promoteApp(t, time.Now())
	app.Update(Event{Kind: EventToolCall, ID: "c1", Tool: "subagent", Text: "go-docs: write the docs", Promote: true})
	app.Update(Event{Kind: EventToolResult, ID: "c1", Text: "wrote docs/verification.md"})
	plain := ansi.Strip(app.View().Content)
	if !strings.Contains(plain, "⟩ subagent go-docs: write the docs") {
		t.Fatalf("the finished child did not promote to its own row\n%s", plain)
	}
}

func TestAnAskedChildPromotesToChatOnTheSwitchAlone(t *testing.T) {
	app := promoteApp(t, time.Now())
	app.Update(Event{Kind: EventToolCall, ID: "c1", Tool: "subagent", Text: "go-docs: rewrite the guide", Promote: true})
	app.Update(Event{Kind: EventDecision, Decision: &session.Decision{Tool: "subagent", Verdict: session.Ask}})
	plain := ansi.Strip(app.View().Content)
	if !strings.Contains(plain, "⟩ subagent go-docs: rewrite the guide") {
		t.Fatalf("the asked child did not promote to its own row\n%s", plain)
	}
}

func TestAFailedChildPromotesToChatOnTheSwitchAlone(t *testing.T) {
	app := promoteApp(t, time.Now())
	app.Update(Event{Kind: EventToolCall, ID: "c1", Tool: "subagent", Text: "go-docs: write the docs", Promote: true})
	app.Update(Event{Kind: EventToolResult, ID: "c1", Text: "the child errored", Failed: true})
	plain := ansi.Strip(app.View().Content)
	if !strings.Contains(plain, "⟩ subagent go-docs: write the docs") {
		t.Fatalf("the failed child did not promote to its own row\n%s", plain)
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
