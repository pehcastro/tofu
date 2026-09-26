package tui

import (
	"context"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"tofu/internal/sys"
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

func pastedLong(t *testing.T, tasks chan string, long string) *App {
	t.Helper()
	board, _ := pasteBoard(t)
	board.Read = func() (sys.Clipboard, error) { return sys.Clipboard{Kind: sys.ClipboardText, Text: long}, nil }
	app := newTestApp(Options{
		Repo:  testRepo,
		Wires: anthropicAlone,
		Now:   fixedClock(),
		Paste: board,
		Turn: func(_ context.Context, _ Pick, task string, _ CalledFromInsideTheTurnAndNeverAfterItReturns) {
			tasks <- task
		},
	})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return app
}

func pasteInto(app *App) {
	_, cmd := app.Update(tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl})
	app.Update(cmd())
}

func TestARecalledEntryKeepsItsTextChipAndSendsTheTextNotTheToken(t *testing.T) {
	long := strings.Repeat("the gate reads the policy before the wire. ", 10)
	tasks := make(chan string, 8)
	app := pastedLong(t, tasks, long)
	pasteInto(app)
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if sent := <-tasks; sent != long {
		t.Fatalf("the first send carried %q, want the pasted text", sent)
	}
	endTurn(t, app)
	pressUp(app)
	if got, want := app.view.Value(), "[Text "+strconv.Itoa(len([]rune(long)))+" characters]"; got != want {
		t.Fatalf("up recalled %q, want the chip %q", got, want)
	}
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if sent := <-tasks; sent != long {
		t.Fatalf("re-sending a recalled entry carried %q, want the pasted text", sent)
	}
}

func TestAChipNeverLeaksFromOneRecalledEntryIntoAnother(t *testing.T) {
	long := strings.Repeat("the gate reads the policy before the wire. ", 10)
	tasks := make(chan string, 8)
	app := pastedLong(t, tasks, long)
	pasteInto(app)
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	<-tasks
	endTurn(t, app)
	typeAndSend(app, firstTask)
	<-tasks
	endTurn(t, app)
	pressUp(app)
	pressUp(app)
	pressDown(app)
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if sent := <-tasks; sent != firstTask {
		t.Fatalf("the plain entry sent after walking past a chip carried %q, want %q", sent, firstTask)
	}
}

func TestADraftsChipComesBackWhenHistoryIsWalkedPast(t *testing.T) {
	long := strings.Repeat("the gate reads the policy before the wire. ", 10)
	tasks := make(chan string, 8)
	app := pastedLong(t, tasks, long)
	typeAndSend(app, firstTask)
	<-tasks
	endTurn(t, app)
	pasteInto(app)
	pressUp(app)
	pressDown(app)
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if sent := <-tasks; sent != long {
		t.Fatalf("the draft sent after walking history carried %q, want the pasted text", sent)
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
