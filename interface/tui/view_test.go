package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestTheShellsTabCountsRunningShellsTheWayTheSubAgentsTabCountsSubAgents(t *testing.T) {
	app := sessionApp(t, 120, 36)
	top := strings.Split(ansi.Strip(app.View().Content), "\n")[0]
	if strings.Contains(top, "shells (") {
		t.Fatalf("with no shell the menu reads %q, want plain shells", top)
	}
	app.Update(shellsMsg(shellEntries()))
	top = strings.Split(ansi.Strip(app.View().Content), "\n")[0]
	if !strings.Contains(top, "shells (1)") {
		t.Fatalf("with one running shell and two exited the menu reads %q, want shells (1)", top)
	}
}

func TestAContinuationForkOfTheSameRootKeepsTheHeaderClockAndADifferentRootRestartsIt(t *testing.T) {
	at := fixedStart()
	app := newTestApp(Options{Repo: testRepo, Branch: "develop", Now: func() time.Time { return at }, Wires: anthropicAlone})
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	header := func() string { return strings.Split(ansi.Strip(app.View().Content), "\n")[0] }

	app.Update(Event{Kind: EventSession, Text: "ready-wheat-hare", ID: "b6cb360b1", Root: "b6cb360b1"})
	at = at.Add(3 * time.Hour)
	app.Update(Event{Kind: EventSession, Text: "ready-frost-robin", ID: "c7d1e2f30", Root: "b6cb360b1"})
	at = at.Add(time.Minute)
	if top := header(); !strings.Contains(top, "| 3h 1m ") {
		t.Fatalf("a continuation fork of the same root restarted the header clock: %q", top)
	}

	app.Update(Event{Kind: EventSession, Text: "quiet-oak-fox", ID: "d0e1f2a3b", Root: "d0e1f2a3b"})
	at = at.Add(5 * time.Second)
	if top := header(); !strings.Contains(top, "| 5s ") {
		t.Errorf("a different session line kept the clock of the one before it: %q", top)
	}
}
