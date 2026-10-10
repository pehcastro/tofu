package tui

import "testing"

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
