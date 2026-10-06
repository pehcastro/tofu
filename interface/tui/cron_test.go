package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestALoopIsMadeIdleWithoutATurnAndCronListsIt(t *testing.T) {
	app := newTestApp(Options{Repo: testRepo, Branch: "develop", Now: fixedClock(), Wires: anthropicAlone,
		Turn: func(context.Context, Pick, string, CalledFromInsideTheTurnAndNeverAfterItReturns) {}})
	t.Cleanup(app.options.Host.Close)
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	typeAndSend(app, "/loop 1m check the build")
	typeAndSend(app, "/loop 2m check the docs")
	if app.busy || app.status.Crons != 2 {
		t.Fatalf("making two loops started a turn (%v) or the status counts %d jobs, want 2", app.busy, app.status.Crons)
	}
	typeAndSend(app, "/cron")
	if _, open := app.top().(cronDialog); !open || !strings.Contains(ansi.Strip(app.View().Content), "cron c2 · every 2m") {
		t.Fatalf("/cron drew no list of the jobs:\n%s", ansi.Strip(app.View().Content))
	}
}
