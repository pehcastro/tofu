package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

const promptKeys = "[1] allow once   [2] deny   [3] always here"

func awaitingApp(t *testing.T, answers chan Answer) *App {
	t.Helper()
	at := time.Date(2026, 9, 19, 14, 32, 0, 0, time.UTC)
	app := newTestApp(Options{
		Repo:    testRepo,
		Branch:  "develop",
		Now:     func() time.Time { return at },
		Wires:   anthropicAlone,
		Answers: answers,
		Turn:    func(context.Context, Pick, string, CalledFromInsideTheTurnAndNeverAfterItReturns) {},
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
		{tea.KeyPressMsg{Code: '1', Text: "1"}, AllowedOnce},
		{tea.KeyPressMsg{Code: '2', Text: "2"}, Denied},
		{tea.KeyPressMsg{Code: '3', Text: "3"}, AlwaysHere},
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

func TestAnAskingTurnStillTakesTypingInChat(t *testing.T) {
	const sentence = "and open the changelog after that"
	answers := make(chan Answer, 1)
	app := awaitingApp(t, answers)
	typeText(app, sentence)
	if len(answers) != 0 {
		t.Errorf("typing a sentence under an open ask answered it with %d", <-answers)
	}
	if typed := app.view.Value(); typed != sentence {
		t.Errorf("the composer holds %q, want %q", typed, sentence)
	}
	if !strings.Contains(ansi.Strip(app.View().Content), promptKeys) {
		t.Errorf("typing took the question off the screen\n%s", ansi.Strip(app.View().Content))
	}
}

func TestADigitUnderAnOpenAskTypesOnceTheComposerHasWords(t *testing.T) {
	answers := make(chan Answer, 1)
	app := awaitingApp(t, answers)
	typeText(app, "read 1 file")
	if len(answers) != 0 {
		t.Fatalf("a digit inside a sentence answered the ask with %d", <-answers)
	}
	if typed := app.view.Value(); typed != "read 1 file" {
		t.Fatalf("the composer holds %q, want the digit inside the sentence", typed)
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
