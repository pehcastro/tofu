package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func turningApp(t *testing.T) (*App, chan struct{}) {
	t.Helper()
	started, stopped := make(chan struct{}), make(chan struct{})
	app := newTestApp(Options{
		Repo:   testRepo,
		Branch: "develop",
		Now:    fixedClock(),
		Wires:  anthropicAlone,
		Turn: func(ctx context.Context, _, _ string, _ CalledFromInsideTheTurnAndNeverAfterItReturns) {
			close(started)
			<-ctx.Done()
			close(stopped)
		},
	})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	typeText(app, "rename the judge interface")
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("the turn never started")
	}
	return app, stopped
}

func interrupt(app *App, times int) {
	for range times {
		app.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	}
}

func notes(app *App) int {
	return strings.Count(ansi.Strip(app.View().Content), "· "+stoppingNote)
}

func footer(app *App) string {
	lines := strings.Split(ansi.Strip(app.View().Content), "\n")
	return lines[len(lines)-3]
}

func TestOneInterruptCancelsTheTurnAndWritesOneNote(t *testing.T) {
	app, stopped := turningApp(t)
	interrupt(app, 1)
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("one ctrl+c did not cancel the turn")
	}
	if written := notes(app); written != 1 {
		t.Fatalf("one ctrl+c wrote %d notes, want 1\n%s", written, ansi.Strip(app.View().Content))
	}
}

func TestSevenInterruptsWriteOneNoteAndNotSeven(t *testing.T) {
	app, stopped := turningApp(t)
	interrupt(app, 7)
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("seven ctrl+c did not cancel the turn")
	}
	if written := notes(app); written != 1 {
		t.Fatalf("seven ctrl+c wrote %d notes, want 1\n%s", written, ansi.Strip(app.View().Content))
	}
}

func TestTheFooterSaysWhatASecondInterruptDoes(t *testing.T) {
	app, _ := turningApp(t)
	if running := footer(app); !strings.Contains(running, "ctrl+c stops the turn") {
		t.Errorf("a running turn does not say ctrl+c stops it\n%s", running)
	}
	interrupt(app, 1)
	stopping := footer(app)
	if !strings.Contains(stopping, "ctrl+c will not quit until it ends") {
		t.Errorf("the footer does not say what a second ctrl+c does\n%s", stopping)
	}
	interrupt(app, 1)
	if _, cmd := app.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}); cmd != nil {
		t.Error("a second ctrl+c while stopping produced a command, so it did not wait for the turn")
	}
}
