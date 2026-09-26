package tui

import (
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

var closingShape = regexp.MustCompile(`^.+ \d+s\s+\|\s+waited \d+s\s+\[request#([0-9a-zA-Z-]+)\]$`)

func closingID(t *testing.T, app *App) string {
	t.Helper()
	screen := ansi.Strip(app.View().Content)
	if closings := strings.Count(screen, "waited "); closings != 1 {
		t.Fatalf("%d lines close the turn, want one\n%s", closings, screen)
	}
	for _, line := range strings.Split(screen, "\n") {
		if found := closingShape.FindStringSubmatch(strings.TrimSpace(line)); found != nil {
			return found[1]
		}
	}
	t.Fatalf("no line closes the turn with a duration and an id\n%s", screen)
	return ""
}

func reachesTheFeed(t *testing.T, app *App, id string) {
	t.Helper()
	typeText(app, "#"+id)
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if app.current != screenAgents || app.feed.Selected() != id {
		t.Fatalf("#%s did not reach its card in sub-agents\n%s", id, ansi.Strip(app.View().Content))
	}
}

func TestAnAskingTurnClosesWithADurationAndAnIDThatReachesTheFeed(t *testing.T) {
	answers := make(chan Answer, 1)
	app := awaitingApp(t, answers)
	app.Update(tea.KeyPressMsg{Code: '1', Text: "1"})
	app.Update(Event{Kind: EventToolResult, ID: "c1", Text: "everything up to date"})
	app.Update(Event{Kind: EventDone, Text: "cooked for"})
	app.Update(Closed{})
	reachesTheFeed(t, app, closingID(t, app))
}

func TestAnInterruptedTurnKeepsThePartialInTheFeedAndSaysSoInOneLine(t *testing.T) {
	const partial = "the gate reads the policy first, then the wire, because a locked"
	app, stopped := turningApp(t)
	app.Update(Event{Kind: EventTextDelta, Text: partial})
	interrupt(app, 1)
	<-stopped
	app.Update(Event{Kind: EventDone, Text: "cooked for"})
	app.Update(Closed{})
	chat := ansi.Strip(app.View().Content)
	if strings.Contains(chat, partial) {
		t.Errorf("the half written answer stayed in chat\n%s", chat)
	}
	if !strings.Contains(chat, "64"+charactersKept) {
		t.Errorf("chat does not say where the half written answer went\n%s", chat)
	}
	id := closingID(t, app)
	reachesTheFeed(t, app, id)
	if held := app.happened[app.happenedAt(id)]; held.Body != partial {
		t.Errorf("the feed holds %q under #%s, want the half written answer", held.Body, id)
	}
}

func TestAFailedTurnPutsOneLineInChatAndTheWholeErrorInTheFeed(t *testing.T) {
	const whole = "git push origin develop: exit 128\nfatal: could not read from remote repository\ntransport: ssh: connect to host git.silo port 22: connection refused"
	app, _ := turningApp(t)
	app.Update(Event{Kind: EventFailure, Tool: "bash", Text: whole})
	app.Update(Event{Kind: EventDone, Text: "failed after"})
	app.Update(Closed{})
	chat := ansi.Strip(app.View().Content)
	if drawn := strings.Count(chat, "fatal: could not read"); drawn != 0 {
		t.Errorf("the whole error spilled into chat\n%s", chat)
	}
	id := closingID(t, app)
	reachesTheFeed(t, app, id)
	if held := app.happened[app.happenedAt(id)]; held.Body != whole {
		t.Errorf("the feed holds %q under #%s, want the whole error", held.Body, id)
	}
}
