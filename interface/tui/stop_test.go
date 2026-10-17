package tui

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/fixture"
	"tofu/interface/tui/subagent"
	"tofu/internal/host"
	roster "tofu/internal/subagent"
)

type steppedClock struct {
	mu sync.Mutex
	at time.Time
}

func (c *steppedClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *steppedClock) pass(by time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(by)
}

func stuckLead(t *testing.T) (*App, *steppedClock, <-chan struct{}) {
	t.Helper()
	clock := &steppedClock{at: fixture.Opened()}
	cancelled, stuck := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() { close(stuck) })
	playing, _ := host.New(host.Config{Now: clock.now, Play: func(ctx context.Context, _ Pick, _ string, live host.Live) {
		live.Emit(Event{Kind: EventRequesting})
		live.Emit(Event{Kind: EventText, Text: "three pieces, three sub-agents"})
		live.Emit(Event{Kind: EventSubAgent, SubAgents: runningSubAgent()})
		go func() {
			select {
			case <-ctx.Done():
				close(cancelled)
			case <-stuck:
			}
		}()
		<-stuck
	}})
	t.Cleanup(playing.Close)
	app := newTestApp(Options{Repo: testRepo, Branch: "develop", Now: clock.now, Wires: anthropicAlone, Host: playing})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	typeAndSend(app, "hand the three pieces to sub-agents")
	pumpUntil(t, app, EventSubAgent)
	return app, clock, cancelled
}

func drawn(t *testing.T, app *App, when string) {
	t.Helper()
	t.Logf("%s, at 80x24:\n%s", when, ansi.Strip(app.View().Content))
}

func quits(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, quit := cmd().(tea.QuitMsg)
	return quit
}

func TestStopEscFewSecondsIntoALeadStopTheTurnIgnoresLeavesTheSubAgentsRunning(t *testing.T) {
	app, clock, cancelled := stuckLead(t)
	esc := tea.KeyPressMsg{Code: tea.KeyEscape}

	app.Update(esc)
	clock.pass(time.Second)
	app.Update(esc)
	clock.pass(3 * time.Second)
	app.Update(esc)
	screen := ansi.Strip(app.View().Content)
	drawn(t, app, "esc, esc one second later, esc four seconds into the lead's stop")
	select {
	case <-cancelled:
		t.Fatal("an esc four seconds into a lead stop the turn ignored stopped everything, want only the lead stopped")
	case <-time.After(500 * time.Millisecond):
	}
	if !strings.Contains(screen, "agents (1)") || !strings.Contains(screen, "stopping the turn") {
		t.Fatal("after the third esc the screen no longer shows the lead stopping beside its running sub-agent")
	}
}

func TestLinkARestoredSubAgentNeverRelabelsANewSpawn(t *testing.T) {
	restored := subagent.Row{Name: "sub-1", Doing: "the first note", State: roster.Finished, Started: fixedClock()().Add(-time.Hour)}
	started := subagent.Row{Name: "sub-2", Doing: "the second note", State: roster.Working}
	app := newTestApp(Options{Repo: testRepo, Branch: "develop", Now: fixedClock(), Wires: bothWires})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	for _, event := range []Event{
		{Kind: EventSubAgent, SubAgents: []subagent.Row{restored}},
		{Kind: EventTurnStarted},
		{Kind: EventRequesting},
		{Kind: EventToolCall, ID: "call-9", Tool: "spawn", Text: "the second note", Promote: true},
		{Kind: EventSubAgent, SubAgents: []subagent.Row{restored, started}},
		{Kind: EventToolResult, ID: "call-9", Text: "sub-2 is running", SubAgents: []subagent.Row{started}},
	} {
		app.Update(event)
	}
	drawn(t, app, "a restored sub-1, then a spawn that started sub-2")
	if spawned := rowHolding(t, app, "[&sub-"); strings.Count(spawned, "[&sub-2]") != 1 || strings.Contains(spawned, "[&sub-1]") {
		t.Fatalf("the new spawn's chip reads %q, want sub-2 and never the restored sub-1", spawned)
	}
}

func TestStopCtrlCFourSecondsIntoALeadStopAsksBeforeItStopsEverything(t *testing.T) {
	app, clock, cancelled := stuckLead(t)
	ctrlC := tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}

	app.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	clock.pass(4 * time.Second)
	if quits(app.update(ctrlC)) {
		t.Fatal("the first ctrl+c four seconds into a lead stop quit, want it to ask first")
	}
	screen := ansi.Strip(app.View().Content)
	drawn(t, app, "esc, then ctrl+c four seconds later")
	select {
	case <-cancelled:
		t.Fatal("the first ctrl+c four seconds into a lead stop stopped everything without asking")
	case <-time.After(500 * time.Millisecond):
	}
	if !strings.Contains(screen, "press Ctrl+C again to confirm") {
		t.Fatal("the first ctrl+c four seconds into a lead stop did not ask to stop the sub-agents")
	}
	if quits(app.update(ctrlC)) {
		t.Fatal("the confirming ctrl+c quit, want it to stop everything")
	}
	drawn(t, app, "the confirming ctrl+c")
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("the confirming ctrl+c did not stop everything")
	}
	if !quits(app.update(ctrlC)) {
		drawn(t, app, "a third ctrl+c")
		t.Fatal("a ctrl+c with the turn still running after stop everything did not quit")
	}
}
