package tui

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"tofu/internal/golden"
	"tofu/internal/host"
)

var promptKeys = regexp.MustCompile(`\[1\] allow once\s+\[2\] deny\s+\[3\] always here\s+\[4\] never here\s+\[5\] cancel`)

func awaitingApp(t *testing.T) (*App, <-chan Answer) {
	t.Helper()
	at := time.Date(2026, 9, 19, 14, 32, 0, 0, time.UTC)
	given := make(chan (<-chan Answer), 1)
	playing, _ := host.New(host.Config{Now: func() time.Time { return at }, Play: func(_ context.Context, _ Pick, _ string, live host.Live) { given <- live.Answers }})
	app := newTestApp(Options{
		Repo:   testRepo,
		Branch: "develop",
		Now:    func() time.Time { return at },
		Wires:  anthropicAlone,
		Host:   playing,
	})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	typeText(app, "push the branch")
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	app.Update(Event{Kind: EventToolCall, ID: "c1", Tool: "bash", Text: "git push --force origin main"})
	app.Update(Event{Kind: EventDecision, Decision: asked()})
	app.Update(Event{Kind: EventAwaitPerson})
	return app, <-given
}

func TestAnAwaitingAskDrawsTheFiveKeysItTakes(t *testing.T) {
	app, _ := awaitingApp(t)
	content := app.View().Content
	if !promptKeys.MatchString(ansi.Strip(content)) {
		t.Fatalf("the awaiting ask does not offer its keys\n%s", ansi.Strip(content))
	}
	golden.Assert(t, "session-awaiting-80x24.golden", content)
}

func TestTheAnswerKeysEachSendTheirOwnAnswer(t *testing.T) {
	for _, pressed := range []struct {
		key  tea.KeyPressMsg
		want Answer
	}{
		{tea.KeyPressMsg{Code: '1', Text: "1"}, AllowedOnce},
		{tea.KeyPressMsg{Code: '2', Text: "2"}, Denied},
		{tea.KeyPressMsg{Code: '3', Text: "3"}, AlwaysHere},
		{tea.KeyPressMsg{Code: '4', Text: "4"}, host.NeverHere},
	} {
		app, answers := awaitingApp(t)
		app.Update(pressed.key)
		select {
		case got := <-answers:
			if got != pressed.want {
				t.Errorf("%q answered %d, want %d", pressed.key.String(), got, pressed.want)
			}
		default:
			t.Errorf("%q sent no answer at all", pressed.key.String())
		}
		if promptKeys.MatchString(ansi.Strip(app.View().Content)) {
			t.Errorf("%q was answered and the prompt is still on the screen", pressed.key.String())
		}
	}
}

func TestAnAskingTurnStillTakesTypingInChat(t *testing.T) {
	const sentence = "and open the changelog after that"
	app, answers := awaitingApp(t)
	typeText(app, sentence)
	if len(answers) != 0 {
		t.Errorf("typing a sentence under an open ask answered it with %d", <-answers)
	}
	if typed := app.view.Value(); typed != sentence {
		t.Errorf("the composer holds %q, want %q", typed, sentence)
	}
	if !promptKeys.MatchString(ansi.Strip(app.View().Content)) {
		t.Errorf("typing took the question off the screen\n%s", ansi.Strip(app.View().Content))
	}
}

func TestADigitUnderAnOpenAskTypesOnceTheComposerHasWords(t *testing.T) {
	app, answers := awaitingApp(t)
	typeText(app, "read 1 file")
	if len(answers) != 0 {
		t.Fatalf("a digit inside a sentence answered the ask with %d", <-answers)
	}
	if typed := app.view.Value(); typed != "read 1 file" {
		t.Fatalf("the composer holds %q, want the digit inside the sentence", typed)
	}
}

func TestInterruptStillReachesTheTurnWhileItWaits(t *testing.T) {
	app, answers := awaitingApp(t)
	app.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if len(answers) != 0 {
		t.Fatal("ctrl+c was taken as an answer to the ask")
	}
	if !strings.Contains(ansi.Strip(app.View().Content), "stopping the turn") {
		t.Fatalf("ctrl+c did not stop the turn while it waited\n%s", ansi.Strip(app.View().Content))
	}
}
