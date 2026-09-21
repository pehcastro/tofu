package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func historyApp(t *testing.T) *App {
	t.Helper()
	return queueApp(t, make(chan string, 4))
}

func pressUp(app *App)   { app.Update(tea.KeyPressMsg{Code: tea.KeyUp}) }
func pressDown(app *App) { app.Update(tea.KeyPressMsg{Code: tea.KeyDown}) }

func TestUpRecallsTheLastSentMessageAndWalksBack(t *testing.T) {
	app := historyApp(t)
	typeAndSend(app, firstTask)
	typeAndSend(app, secondTask)

	pressUp(app)
	if got := app.view.Value(); got != secondTask {
		t.Fatalf("the first up holds %q, want the last sent message %q", got, secondTask)
	}
	pressUp(app)
	if got := app.view.Value(); got != firstTask {
		t.Fatalf("the second up holds %q, want the message before it %q", got, firstTask)
	}
	pressUp(app)
	if got := app.view.Value(); got != firstTask {
		t.Fatalf("up past the oldest message changed the composer to %q", got)
	}
}

func TestDownWalksForwardAndPastTheNewestRestoresTheDraft(t *testing.T) {
	app := historyApp(t)
	typeAndSend(app, firstTask)
	typeAndSend(app, secondTask)

	pressUp(app)
	pressUp(app)
	if got := app.view.Value(); got != firstTask {
		t.Fatalf("two ups hold %q, want %q", got, firstTask)
	}
	pressDown(app)
	if got := app.view.Value(); got != secondTask {
		t.Fatalf("one down holds %q, want %q", got, secondTask)
	}
	pressDown(app)
	if got := app.view.Value(); got != "" {
		t.Fatalf("down past the newest holds %q, want the empty draft that was there", got)
	}
}

func TestADraftIsNeverLostByPressingUp(t *testing.T) {
	app := historyApp(t)
	typeAndSend(app, firstTask)

	typeText(app, "a draft not yet sent")
	pressUp(app)
	if got := app.view.Value(); got != firstTask {
		t.Fatalf("up over a draft holds %q, want the last sent message %q", got, firstTask)
	}
	pressDown(app)
	if got := app.view.Value(); got != "a draft not yet sent" {
		t.Fatalf("down past the newest lost the draft, holds %q", got)
	}
}
