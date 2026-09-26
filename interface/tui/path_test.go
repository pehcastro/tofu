package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func repoPaths() []string {
	return []string{
		"internal/turn/loop.go",
		"internal/judge/policy/toolgate.go",
		"interface/tui/app.go",
		"CLAUDE.md",
	}
}

func pathApp(t *testing.T, entered chan<- string) *App {
	t.Helper()
	app := newTestApp(Options{
		Repo:   testRepo,
		Branch: "develop",
		Now:    fixedClock(),
		Wires:  anthropicAlone,
		Paths:  repoPaths,
		Turn: func(_ context.Context, _ Pick, task string, _ CalledFromInsideTheTurnAndNeverAfterItReturns) {
			entered <- task
		},
	})
	for _, cmd := range []tea.Cmd{app.Init()} {
		app.Update(cmd())
	}
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return app
}

func sent(t *testing.T, entered <-chan string) string {
	t.Helper()
	select {
	case task := <-entered:
		return task
	case <-time.After(2 * time.Second):
		t.Fatal("nothing reached the model")
	}
	return ""
}

func TestAnAtInsideAWordOpensNothing(t *testing.T) {
	entered := make(chan string, 1)
	app := pathApp(t, entered)
	typeText(app, "mail pehcastro@internal")
	if picked, open := app.view.Picked(); open {
		t.Fatalf("an email address opened a picker on %q", picked)
	}
	if listed := ansi.Strip(app.View().Content); strings.Contains(listed, "internal/turn/loop.go") {
		t.Errorf("an email address listed a path it could have matched\n%s", listed)
	}
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if task := sent(t, entered); task != "mail pehcastro@internal" {
		t.Errorf("the model was sent %q", task)
	}
}

func TestADotSlashAndADotDotSlashCompleteTheSameWayAsAnAt(t *testing.T) {
	for _, sigil := range []string{"./", "../"} {
		t.Run(sigil, func(t *testing.T) {
			entered := make(chan string, 1)
			app := pathApp(t, entered)
			typeText(app, sigil+"int")
			if !strings.Contains(ansi.Strip(app.View().Content), sigil+"internal/turn/loop.go") {
				t.Errorf("%s int lists nothing\n%s", sigil, ansi.Strip(app.View().Content))
			}
			app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			if typed := app.view.Value(); typed != sigil+"internal/turn/loop.go" {
				t.Errorf("accepting the completion left %q in the composer", typed)
			}
		})
	}
}

func TestABackslashEscapesTheSigilAndTheModelReadsItWithoutTheBackslash(t *testing.T) {
	entered := make(chan string, 1)
	app := pathApp(t, entered)
	typeText(app, `mail me at \@tofu`)
	if picked, open := app.view.Picked(); open {
		t.Fatalf("an escaped sigil opened a picker on %q", picked)
	}
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if task := sent(t, entered); task != "mail me at @tofu" {
		t.Errorf("the model was sent %q, want the backslash gone", task)
	}
}
