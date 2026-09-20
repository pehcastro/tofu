package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

const promptKeys = "[a] allow once   [d] deny   [A] always here"

func awaitingApp(t *testing.T, answers chan Answer) *App {
	t.Helper()
	at := time.Date(2026, 9, 19, 14, 32, 0, 0, time.UTC)
	app := newTestApp(Options{
		Repo:    "silo",
		Branch:  "develop",
		Now:     func() time.Time { return at },
		Wires:   anthropicAlone,
		Answers: answers,
		Turn:    func(context.Context, string, string, func(Event)) {},
	})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	typeText(app, "push the branch")
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	app.Update(Event{Kind: EventToolCall, ID: "c1", Tool: "bash", Text: "git push --force origin main"})
	app.Update(Event{Kind: EventDecision, Decision: asked()})
	app.Update(Event{Kind: EventAwaitPerson})
	return app
}

func TestAnAwaitingAskDrawsTheThreeKeysItTakes(t *testing.T) {
	app := awaitingApp(t, make(chan Answer, 1))
	content := app.View().Content
	if !strings.Contains(ansi.Strip(content), promptKeys) {
		t.Fatalf("the awaiting ask does not offer its keys\n%s", ansi.Strip(content))
	}
	assertGolden(t, "session-awaiting-80x24.golden", content)
}

func TestTheAnswerKeysEachSendTheirOwnAnswer(t *testing.T) {
	for _, pressed := range []struct {
		key  tea.KeyPressMsg
		want Answer
	}{
		{tea.KeyPressMsg{Code: 'a', Text: "a"}, AllowedOnce},
		{tea.KeyPressMsg{Code: 'd', Text: "d"}, Denied},
		{tea.KeyPressMsg{Code: 'a', Text: "A", Mod: tea.ModShift}, AlwaysHere},
	} {
		answers := make(chan Answer, 1)
		app := awaitingApp(t, answers)
		app.Update(pressed.key)
		select {
		case got := <-answers:
			if got != pressed.want {
				t.Errorf("%q answered %d, want %d", pressed.key.String(), got, pressed.want)
			}
		default:
			t.Errorf("%q sent no answer at all", pressed.key.String())
		}
		if strings.Contains(ansi.Strip(app.View().Content), promptKeys) {
			t.Errorf("%q was answered and the prompt is still on the screen", pressed.key.String())
		}
	}
}

func TestWhileAwaitingAKeyThatIsNotAnAnswerDoesNothing(t *testing.T) {
	for _, pressed := range []tea.KeyPressMsg{
		{Code: 'w', Text: "w"},
		{Code: 'x', Text: "x"},
		{Code: tea.KeyEnter},
		{Code: tea.KeyTab},
		{Code: '2', Mod: tea.ModAlt},
	} {
		answers := make(chan Answer, 1)
		app := awaitingApp(t, answers)
		before := app.View().Content
		app.Update(pressed)
		if len(answers) != 0 {
			t.Errorf("%q resolved the ask", pressed.String())
		}
		if after := app.View().Content; after != before {
			t.Errorf("%q changed the screen\n--- after ---\n%s\n--- before ---\n%s", pressed.String(), after, before)
		}
		if !strings.Contains(ansi.Strip(app.View().Content), promptKeys) {
			t.Errorf("%q left the prompt no longer waiting", pressed.String())
		}
	}
}

func TestInterruptStillReachesTheTurnWhileItWaits(t *testing.T) {
	answers := make(chan Answer, 1)
	app := awaitingApp(t, answers)
	app.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if len(answers) != 0 {
		t.Fatal("ctrl+c was taken as an answer to the ask")
	}
	if !strings.Contains(ansi.Strip(app.View().Content), "stopping the turn") {
		t.Fatalf("ctrl+c did not stop the turn while it waited\n%s", ansi.Strip(app.View().Content))
	}
}
