package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"tofu/internal/sys"
)

const (
	copiedAnswer = "the gate reads the policy before the wire, and here is why."
	copyRefusal  = "another program is holding the clipboard"
)

type stubBoard struct {
	written string
	writes  int
	refuse  error
}

func (b *stubBoard) write(text string) error {
	b.writes++
	if b.refuse != nil {
		return b.refuse
	}
	b.written = text
	return nil
}

func copyApp(t *testing.T, board *stubBoard) *App {
	t.Helper()
	at := time.Date(2026, 9, 19, 14, 32, 0, 0, time.UTC)
	app := newTestApp(Options{
		Repo:   testRepo,
		Branch: "develop",
		Now:    func() time.Time { return at },
		Wires:  anthropicAlone,
		Copy:   board.write,
		Turn:   func(context.Context, Pick, string, CalledFromInsideTheTurnAndNeverAfterItReturns) {},
	})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	app.Update(Event{Kind: EventToolCall, ID: "c1", Tool: "bash", Text: longIntent, Detail: longCommand})
	app.Update(Event{Kind: EventToolResult, ID: "c1", Text: "14 lines, 64 bytes"})
	app.Update(Event{Kind: EventText, Text: copiedAnswer})
	return app
}

func press(t *testing.T, app *App, key tea.KeyPressMsg) {
	t.Helper()
	_, cmd := app.Update(key)
	if cmd == nil {
		t.Fatalf("%v produced no command", key)
	}
	app.Update(cmd())
}

func TestAKeyCopiesTheLastAnswer(t *testing.T) {
	board := &stubBoard{}
	app := copyApp(t, board)
	press(t, app, tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	if board.written != copiedAnswer {
		t.Errorf("ctrl+y wrote %q to the clipboard, want the last answer", board.written)
	}
	if !strings.Contains(app.status.Note, "the last answer copied") {
		t.Errorf("the status line does not say the answer was copied: %q", app.status.Note)
	}
	if transcript := ansi.Strip(app.view.View()); strings.Contains(transcript, "the last answer copied") {
		t.Errorf("a copy wrote a transcript entry\n%s", transcript)
	}
}

func TestAKeyCopiesTheOpenCallWithItsResult(t *testing.T) {
	board := &stubBoard{}
	app := copyApp(t, board)
	press(t, app, tea.KeyPressMsg{Code: 'y', Mod: tea.ModAlt})
	for _, want := range []string{"bash " + longIntent, longCommand, "14 lines, 64 bytes"} {
		if !strings.Contains(board.written, want) {
			t.Errorf("alt+y did not copy %q\n%s", want, board.written)
		}
	}
	if !strings.Contains(app.status.Note, "the tool call copied") {
		t.Errorf("the status line does not say the call was copied: %q", app.status.Note)
	}
}

func TestSlashCopyIsListedAndCopiesWhatTheKeyCopies(t *testing.T) {
	board := &stubBoard{}
	app := copyApp(t, board)
	typeText(app, "/")
	if listed := ansi.Strip(app.View().Content); !strings.Contains(listed, "/copy") {
		t.Errorf("a bare slash does not list /copy\n%s", listed)
	}
	typeText(app, "copy")
	press(t, app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if board.written != copiedAnswer {
		t.Errorf("/copy wrote %q to the clipboard, want the last answer", board.written)
	}
	if !strings.Contains(app.status.Note, "the last answer copied") {
		t.Errorf("the status line does not say the answer was copied: %q", app.status.Note)
	}
}

func TestWithNoLocalClipboardTheTerminalCarriesTheCopyAndIsNotCalledASuccess(t *testing.T) {
	board := &stubBoard{refuse: sys.ErrNoLocalClipboard}
	app := copyApp(t, board)
	_, cmd := app.Update(tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	_, handed := app.Update(cmd())
	if handed == nil {
		t.Fatal("nothing was handed to the terminal")
	}
	if text := fmt.Sprintf("%v", handed()); text != copiedAnswer {
		t.Errorf("the terminal was handed %q, want the last answer", text)
	}
	if !strings.Contains(app.status.Note, "does not say whether it took it") {
		t.Errorf("the status line claims a copy the terminal never confirmed: %q", app.status.Note)
	}
}

func TestACopyThatFailsSaysTheCopyDidNotHappen(t *testing.T) {
	board := &stubBoard{refuse: errors.New(copyRefusal)}
	app := copyApp(t, board)
	press(t, app, tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	if want := "the last answer was not copied: " + copyRefusal; app.status.Note != want {
		t.Errorf("status.Note = %q, want %q", app.status.Note, want)
	}
	if strings.Contains(app.status.Note, sysPrefix) {
		t.Errorf("the status line still names the package: %q", app.status.Note)
	}
	if transcript := ansi.Strip(app.view.View()); strings.Contains(transcript, "copied,") || strings.Contains(transcript, copyRefusal) {
		t.Errorf("a refused clipboard write reached the transcript\n%s", transcript)
	}
}

func TestRepeatedCopiesReplaceTheStatusLineRatherThanStack(t *testing.T) {
	board := &stubBoard{}
	app := copyApp(t, board)
	for range 3 {
		press(t, app, tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	}
	if board.writes != 3 {
		t.Fatalf("three presses wrote %d times, want 3", board.writes)
	}
	if count := strings.Count(app.status.Note, "copied,"); count != 1 {
		t.Errorf("status.Note stacked %d results: %q", count, app.status.Note)
	}
}

func TestASuccessThenARefusalLeavesNoStaleSuccess(t *testing.T) {
	board := &stubBoard{}
	app := copyApp(t, board)
	press(t, app, tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	if !strings.Contains(app.status.Note, "copied,") {
		t.Fatalf("the first copy did not report success: %q", app.status.Note)
	}
	board.refuse = errors.New(copyRefusal)
	press(t, app, tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	if strings.Contains(app.status.Note, "copied,") {
		t.Errorf("a stale success survived a later refusal: %q", app.status.Note)
	}
	if !strings.Contains(app.status.Note, copyRefusal) {
		t.Errorf("status.Note = %q, want the refusal", app.status.Note)
	}
}

func TestANonCopyKeyClearsAStaleStatusLine(t *testing.T) {
	board := &stubBoard{}
	app := copyApp(t, board)
	press(t, app, tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	if app.status.Note == "" {
		t.Fatal("the copy left nothing to clear")
	}
	app.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if app.status.Note != "" {
		t.Errorf("status.Note = %q after an unrelated key, want cleared", app.status.Note)
	}
}

func TestWithNothingToCopyTheKeySaysSoAndWritesNothing(t *testing.T) {
	board := &stubBoard{}
	app := newTestApp(Options{Repo: testRepo, Branch: "develop", Now: fixedClock(), Wires: anthropicAlone, Copy: board.write})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	app.Update(tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	if board.writes != 0 {
		t.Errorf("the clipboard was written %d times with nothing to copy", board.writes)
	}
	if app.status.Note != nothingToCopy {
		t.Errorf("status.Note = %q, want %q", app.status.Note, nothingToCopy)
	}
	if transcript := ansi.Strip(app.view.View()); strings.Contains(transcript, nothingToCopy) {
		t.Errorf("nothing-to-copy reached the transcript\n%s", transcript)
	}
}
