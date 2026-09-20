package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestTheSetupScreenPicksUpALoginRunElsewhere(t *testing.T) {
	left := setupRequirements()
	app := newTestApp(Options{
		Repo:         "silo",
		Now:          fixedClock(),
		Requirements: left,
		Recheck:      func() []Requirement { return left },
		Wires:        func() []Wire { return nil },
	})
	start := app.Init()
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if start == nil {
		t.Fatal("the setup screen starts no watch of its own")
	}
	if !strings.Contains(ansi.Strip(app.View().Content), setupWatch) {
		t.Fatalf("the setup screen never says a login elsewhere is noticed\n%s", app.View().Content)
	}

	_, poll := app.Update(recheckMsg{})
	if poll == nil {
		t.Fatal("the watch tick asked for nothing")
	}
	if _, again := app.Update(poll()); again == nil {
		t.Fatal("a check that changed nothing stopped the watch")
	}

	left = nil
	_, checked := app.Update(recheckMsg{})
	app.Update(checked())

	frame := ansi.Strip(app.View().Content)
	if strings.Contains(frame, setupTitle) {
		t.Fatalf("the setup screen is still drawn after the logins were run\n%s", frame)
	}
	if !strings.Contains(frame, readyNote+"silo") {
		t.Fatalf("the app moved on without saying what to type\n%s", frame)
	}
	if app.View().Cursor == nil {
		t.Fatal("the composer has no cursor, so it did not take focus when setup cleared")
	}
}

func TestTheEmptyComposerSaysWhatToType(t *testing.T) {
	app := newTestApp(Options{Repo: "silo", Branch: "develop", Now: fixedClock(), Wires: anthropicAlone})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	view := app.View()
	frame := ansi.Strip(view.Content)
	for _, want := range []string{readyNote + "silo", "what should tofu do here?", "⏎ send"} {
		if !strings.Contains(frame, want) {
			t.Errorf("the empty session at 80 columns does not say %q\n%s", want, frame)
		}
	}
	if view.Cursor == nil {
		t.Error("the empty composer has no cursor, so it never took focus")
	}
}
