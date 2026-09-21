package tui

import (
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

var closingShape = regexp.MustCompile(`^· .+ \d+s( · waited \d+s)? · \[#([0-9a-f]+)\]$`)

func closingID(t *testing.T, app *App) string {
	t.Helper()
	for _, line := range strings.Split(ansi.Strip(app.View().Content), "\n") {
		if found := closingShape.FindStringSubmatch(strings.TrimRight(line, " ")); found != nil {
			return found[2]
		}
	}
	t.Fatalf("no line closes the turn with a duration and an id\n%s", ansi.Strip(app.View().Content))
	return ""
}

func reachesWork(t *testing.T, app *App, id string) {
	t.Helper()
	typeText(app, "#"+id)
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if app.current != viewWork {
		t.Fatalf("#%s did not reach work\n%s", id, ansi.Strip(app.View().Content))
	}
}

func TestAnAskingTurnClosesWithADurationAndAnIDThatReachesWork(t *testing.T) {
	answers := make(chan Answer, 1)
	app := awaitingApp(t, answers)
	app.Update(tea.KeyPressMsg{Code: '1', Text: "1"})
	app.Update(Event{Kind: EventToolResult, ID: "c1", Text: "everything up to date"})
	app.Update(Event{Kind: EventDone, Text: "cooked for"})
	app.Update(Closed{})
	reachesWork(t, app, closingID(t, app))
}

func TestAnInterruptedTurnKeepsThePartialInWorkAndSaysSoInOneLine(t *testing.T) {
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
	if !strings.Contains(chat, "64 characters were written and kept in work") {
		t.Errorf("chat does not say where the half written answer went\n%s", chat)
	}
	id := closingID(t, app)
	reachesWork(t, app, id)
	if held, _ := app.work.Picked(); held.Output != partial {
		t.Errorf("work holds %q under #%s, want the half written answer", held.Output, id)
	}
}

func TestAFailedTurnPutsOneLineInChatAndTheWholeErrorInWork(t *testing.T) {
	const whole = "git push origin develop: exit 128\nfatal: could not read from remote repository\ntransport: ssh: connect to host git.silo port 22: connection refused"
	app, _ := turningApp(t)
	app.Update(Event{Kind: EventFailure, Tool: "bash", Text: whole})
	app.Update(Event{Kind: EventDone, Text: "failed after"})
	app.Update(Closed{})
	chat := ansi.Strip(app.View().Content)
	if drawn := strings.Count(chat, "! "); drawn != 1 {
		t.Errorf("the failure drew %d lines in chat, want one\n%s", drawn, chat)
	}
	if !strings.Contains(chat, "the whole error is in work") {
		t.Errorf("chat does not say where the whole error went\n%s", chat)
	}
	id := closingID(t, app)
	reachesWork(t, app, id)
	if held, _ := app.work.Picked(); held.Output != whole {
		t.Errorf("work holds %q under #%s, want the whole error", held.Output, id)
	}
}
