package tui

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"boji/interface/tui/frame"
)

var update = flag.Bool("update", false, "rewrite the golden files")

func fixedClock() func() time.Time {
	at := time.Date(2026, 9, 19, 14, 32, 0, 0, time.UTC)
	return func() time.Time { return at.Add(138 * time.Second) }
}

func sessionApp(t *testing.T, width, height int) *App {
	t.Helper()
	app := New(Options{Repo: "silo", Branch: "develop", Model: "claude-opus-5", Now: fixedClock()})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: width, Height: height})
	app.Update(frame.Quota{
		Label:    "anthropic 7d",
		Fraction: 0.62,
		Reported: true,
		ResetsAt: time.Date(2026, 9, 19, 18, 0, 0, 0, time.Local),
	})
	for _, event := range []Event{
		{Kind: EventStats, Model: "claude-opus-5-20260901", TokensIn: 284000, TokensOut: 61000, Decisions: 3},
		{Kind: EventText, Text: "reading the gate first, then the policy that decides it."},
		{Kind: EventToolCall, Tool: "read", Text: "internal/judge/policy/toolgate.go"},
		{Kind: EventToolResult, Text: "package policy  412 bytes"},
		{Kind: EventToolCall, Tool: "bash", Text: "go test ./internal/judge/..."},
		{Kind: EventToolResult, Text: "ok boji/internal/judge 0.42s  64 bytes"},
		{Kind: EventDone, Text: "turn stopped, 3 steps, 8412 ms, quota windows five_hour and seven_day"},
	} {
		app.Update(event)
	}
	return app
}

func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != got {
		t.Errorf("%s does not match the golden file\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

func TestSessionViewGolden(t *testing.T) {
	for _, size := range []struct {
		name   string
		width  int
		height int
	}{
		{"session-80x24.golden", 80, 24},
		{"session-120x36.golden", 120, 36},
	} {
		t.Run(size.name, func(t *testing.T) {
			assertGolden(t, size.name, sessionApp(t, size.width, size.height).View().Content)
		})
	}
}

func TestSetupViewGolden(t *testing.T) {
	app := New(Options{
		Repo:  "silo",
		Model: "claude-opus-5",
		Now:   fixedClock(),
		Requirements: []Requirement{{
			What: "there is no anthropic subscription credential, so no model can answer",
			Fix:  "press l to run boji login anthropic, which opens the browser and stores the credential",
		}},
	})
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	assertGolden(t, "setup-80x24.golden", app.View().Content)
}

func typeText(app *App, text string) {
	for _, code := range text {
		app.Update(tea.KeyPressMsg{Code: code, Text: string(code)})
	}
}

func TestInterruptStopsTheTurnAndKeepsTheApp(t *testing.T) {
	cancelled := make(chan struct{})
	app := New(Options{
		Repo: "silo",
		Now:  fixedClock(),
		Turn: func(ctx context.Context, _ string, emit func(Event)) {
			<-ctx.Done()
			close(cancelled)
			emit(Event{Kind: EventNote, Text: "stopped by the operator"})
		},
	})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	typeText(app, "rename the judge interface")

	if _, cmd := app.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd == nil {
		t.Fatal("enter did not start a turn")
	}
	if !app.busy {
		t.Fatal("the app is not running a turn after enter")
	}

	_, cmd := app.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd != nil {
		if msg := cmd(); msg != nil {
			t.Fatalf("ctrl+c during a turn produced %T, want no message", msg)
		}
	}
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("ctrl+c did not cancel the turn")
	}

	for {
		msg := app.waitForEvent()()
		app.Update(msg)
		if _, done := msg.(closedMsg); done {
			break
		}
	}
	if app.busy {
		t.Fatal("the app is still busy after the turn stopped")
	}
	if _, cmd := app.Update(tea.KeyPressMsg{Code: 'x', Text: "x"}); cmd == nil {
		t.Fatal("the app stopped taking keys after ctrl+c")
	}
	if app.View().Content == "" {
		t.Fatal("the app renders nothing after ctrl+c")
	}
}

func TestSecondInterruptQuits(t *testing.T) {
	app := New(Options{Repo: "silo", Now: fixedClock()})
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	_, cmd := app.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+c outside a turn produced no command")
	}
	if _, quit := cmd().(tea.QuitMsg); !quit {
		t.Fatal("ctrl+c outside a turn did not quit")
	}
}
