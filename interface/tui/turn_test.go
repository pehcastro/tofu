package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestAReferenceFollowedFromTheChatRevealsAnEventRecordedWhileTheFeedWasHidden(t *testing.T) {
	at := fixedStart()
	app := phaseApp(t, &at)
	app.Update(Event{Kind: EventToolCall, ID: "call-77", Agent: "go-dev-1", Tool: "read", Text: "a.go"})
	id := short("call-77")
	app.follow("[tool#" + id + "]")
	app.Update(pulseMsg{})
	if app.current != screenAgents || app.feed.Selected() != id {
		t.Fatalf("screen %v selected %q, want the sub-agents screen on %s", app.current, app.feed.Selected(), id)
	}
}

func TestTheLeadsTextShowsWhileItsSpawnResultIsOutstanding(t *testing.T) {
	const said = "reading the gate while c1 works"
	spawned := Event{Kind: EventToolCall, ID: "s1", Tool: "spawn", Text: "read note.txt", Promote: true}
	late := Event{Kind: EventToolResult, ID: "s1", Text: "spawn c1 finished"}
	for name, events := range map[string][]Event{
		"delta before a late result":     {spawned, {Kind: EventRequesting}, {Kind: EventTextDelta, Text: said}, late},
		"text and the result never came": {spawned, {Kind: EventRequesting}, {Kind: EventText, Text: said}},
	} {
		app := newTestApp(Options{Repo: testRepo, Branch: "develop", Now: fixedClock(), Wires: bothWires})
		app.Init()
		app.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
		app.Update(Event{Kind: EventRequesting})
		for _, event := range append(events, Event{Kind: EventDone, Text: "cooked for"}) {
			app.Update(event)
		}
		if transcript := ansi.Strip(app.View().Content); !strings.Contains(transcript, said) {
			t.Errorf("%s: the lead said %q and the chat dropped it\n%s", name, said, transcript)
		}
	}
}
