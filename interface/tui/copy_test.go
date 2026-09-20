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
		Repo:   "silo",
		Branch: "develop",
		Now:    func() time.Time { return at },
		Wires:  anthropicAlone,
		Copy:   board.write,
		Turn:   func(context.Context, string, string, func(Event)) {},
	})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	app.Update(Event{Kind: EventToolCall, ID: "c1", Tool: "bash", Text: longIntent, Detail: longCommand})
	app.Update(Event{Kind: EventToolResult, ID: "c1", Text: "14 lines, 64 bytes"})
	app.Update(Event{Kind: EventText, Text: copiedAnswer})
	return app
}

func press(t *testing.T, app *App, key tea.KeyPressMsg) string {
	t.Helper()
	_, cmd := app.Update(key)
	if cmd == nil {
		t.Fatalf("%v produced no command", key)
	}
	app.Update(cmd())
	return ansi.Strip(app.View().Content)
}

func TestAKeyCopiesTheLastAnswer(t *testing.T) {
	board := &stubBoard{}
	app := copyApp(t, board)
	content := press(t, app, tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	if board.written != copiedAnswer {
		t.Errorf("ctrl+y wrote %q to the clipboard, want the last answer", board.written)
	}
	if !strings.Contains(content, "the last answer copied") {
		t.Errorf("the session does not say the answer was copied\n%s", content)
	}
}

func TestAKeyCopiesTheOpenCallWithItsResult(t *testing.T) {
	board := &stubBoard{}
	app := copyApp(t, board)
	content := press(t, app, tea.KeyPressMsg{Code: 'y', Mod: tea.ModAlt})
	for _, want := range []string{"bash " + longIntent, longCommand, "14 lines, 64 bytes"} {
		if !strings.Contains(board.written, want) {
			t.Errorf("alt+y did not copy %q\n%s", want, board.written)
		}
	}
	if !strings.Contains(content, "the tool call copied") {
		t.Errorf("the session does not say the call was copied\n%s", content)
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
	content := press(t, app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if board.written != copiedAnswer {
		t.Errorf("/copy wrote %q to the clipboard, want the last answer", board.written)
	}
	if !strings.Contains(content, "the last answer copied") {
		t.Errorf("the session does not say the answer was copied\n%s", content)
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
	content := ansi.Strip(app.View().Content)
	if !strings.Contains(content, "does not say whether it took it") {
		t.Errorf("the session claims a copy the terminal never confirmed\n%s", content)
	}
}

func TestACopyThatFailsSaysTheCopyDidNotHappen(t *testing.T) {
	board := &stubBoard{refuse: errors.New(copyRefusal)}
	app := copyApp(t, board)
	content := press(t, app, tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	if !strings.Contains(content, "the copy did not happen: "+copyRefusal) {
		t.Errorf("a refused clipboard write says nothing\n%s", content)
	}
	if strings.Contains(content, "copied,") {
		t.Errorf("a refused clipboard write reported a copy\n%s", content)
	}
}

func TestWithNothingToCopyTheKeySaysSoAndWritesNothing(t *testing.T) {
	board := &stubBoard{}
	app := newTestApp(Options{Repo: "silo", Branch: "develop", Now: fixedClock(), Wires: anthropicAlone, Copy: board.write})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	app.Update(tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	if board.writes != 0 {
		t.Errorf("the clipboard was written %d times with nothing to copy", board.writes)
	}
	if content := ansi.Strip(app.View().Content); !strings.Contains(content, nothingToCopy) {
		t.Errorf("the session does not say there is nothing to copy\n%s", content)
	}
}
