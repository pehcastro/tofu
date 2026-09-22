package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/quote"
)

const quotedEvent = "0f2c9b1a-3333-4aaa-8bbb-ccccccc41099"

func quoteApp(t *testing.T, turns []quote.Turn) *App {
	t.Helper()
	app := newTestApp(Options{Repo: testRepo, Now: fixedClock()})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	app.quote.Set(turns, "")
	app.show(viewQuote)
	return app
}

func TestSlashQuoteOpensThePickerEvenWhenTheRecordHoldsNothing(t *testing.T) {
	entered := make(chan string, 1)
	app := commandApp(t, entered)
	typeText(app, "/quote")
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if app.current != viewQuote {
		t.Fatalf("/quote left the app on view %d, want the quote picker", app.current)
	}
	content := ansi.Strip(app.View().Content)
	if !strings.Contains(content, "0 turns") {
		t.Errorf("the picker does not say it holds nothing\n%s", content)
	}
	nothingEntered(t, entered)
}

func TestEnterOnAPickedTurnWritesTheReferenceAndNothingElse(t *testing.T) {
	app := quoteApp(t, []quote.Turn{{Event: quotedEvent, From: "the agent", Text: "the gate reads library/policy/shell.yaml"}})
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if app.current != viewChat {
		t.Fatalf("picking a turn left the app on view %d, want chat", app.current)
	}
	if typed := app.view.Value(); typed != "[quote#c41099]" {
		t.Fatalf("the composer holds %q, want the reference alone", typed)
	}
}

func TestTheReferenceIsAddedToWhatIsAlreadyTypedRatherThanReplacingIt(t *testing.T) {
	app := quoteApp(t, []quote.Turn{{Event: quotedEvent, From: "the agent", Text: "the gate reads library/policy/shell.yaml"}})
	app.show(viewChat)
	typeText(app, "is this still true ")
	app.show(viewQuote)
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if typed := app.view.Value(); typed != "is this still true [quote#c41099]" {
		t.Fatalf("the composer holds %q", typed)
	}
}

func TestLeavingThePickerWithoutChoosingWritesNothing(t *testing.T) {
	app := quoteApp(t, []quote.Turn{{Event: quotedEvent, From: "the agent", Text: "the gate reads library/policy/shell.yaml"}})
	app.Update(tea.KeyPressMsg{Code: tea.KeyEsc})

	if app.current != viewChat {
		t.Fatalf("esc left the app on view %d", app.current)
	}
	if typed := app.view.Value(); typed != "" {
		t.Fatalf("esc wrote %q into the composer", typed)
	}
}

func TestEnterOnAPickerThatHoldsNothingWritesNothing(t *testing.T) {
	app := quoteApp(t, nil)
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if typed := app.view.Value(); typed != "" {
		t.Fatalf("enter over an empty picker wrote %q", typed)
	}
}
