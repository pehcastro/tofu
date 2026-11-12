package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"tofu/internal/platform/clipboard"
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
	cmd := app.update(key)
	if cmd == nil {
		t.Fatalf("%v produced no command", key)
	}
	app.update(cmd())
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
	board := &stubBoard{refuse: clipboard.ErrNoLocalClipboard}
	app := copyApp(t, board)
	cmd := app.update(tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	handed := app.update(cmd())
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

func longAnswerApp(t *testing.T, board *stubBoard) *App {
	t.Helper()
	app := newTestApp(Options{Repo: testRepo, Branch: "develop", Now: fixedClock(), Wires: anthropicAlone, Copy: board.write})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	var answer []string
	for row := 1; row <= 200; row++ {
		answer = append(answer, fmt.Sprintf("- line %03d of the long answer", row))
	}
	app.Update(Event{Kind: EventText, Text: strings.Join(answer, "\n")})
	app.Update(Event{Kind: EventDone, Text: "cooked for"})
	return app
}

func screenRow(app *App, y int) string {
	return strings.Trim(strings.Split(ansi.Strip(app.View().Content), "\n")[y], " │┃")
}

func released(app *App, x, y int) {
	if cmd := app.update(tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft}); cmd != nil {
		app.update(cmd())
	}
}

func TestADragHeldAcrossTheWheelCopiesBothScreens(t *testing.T) {
	board := &stubBoard{}
	app := longAnswerApp(t, board)
	app.update(tea.KeyPressMsg{Code: tea.KeyHome})
	first := screenRow(app, 3)
	app.update(tea.MouseClickMsg{X: 4, Y: 3, Button: tea.MouseLeft})
	app.update(tea.MouseMotionMsg{X: 20, Y: 6, Button: tea.MouseLeft})
	held := ansi.Strip(app.View().Content)
	for range 10 {
		app.update(tea.MouseWheelMsg{X: 20, Y: 6, Button: tea.MouseWheelDown})
	}
	if ansi.Strip(app.View().Content) == held {
		t.Fatal("the wheel during a drag moved nothing on the screen")
	}
	app.update(tea.MouseMotionMsg{X: 70, Y: 10, Button: tea.MouseLeft})
	last := screenRow(app, 10)
	released(app, 70, 10)
	lines := strings.Split(board.written, "\n")
	if strings.TrimSpace(lines[0]) != first || strings.TrimSpace(lines[len(lines)-1]) != last {
		t.Fatalf("copied %d lines from %q to %q, want %q to %q", len(lines), lines[0], lines[len(lines)-1], first, last)
	}
	if len(lines) != 30+10-3+1 {
		t.Fatalf("copied %d lines, want the 38 between the press and the release", len(lines))
	}
}

func TestADragHeldAtTheBottomEdgeScrollsAndGrows(t *testing.T) {
	board := &stubBoard{}
	app := longAnswerApp(t, board)
	app.update(tea.KeyPressMsg{Code: tea.KeyHome})
	app.update(tea.MouseClickMsg{X: 4, Y: 3, Button: tea.MouseLeft})
	tick := app.update(tea.MouseMotionMsg{X: 40, Y: 23, Button: tea.MouseLeft})
	before := ansi.Strip(app.View().Content)
	for range 5 {
		if tick == nil {
			t.Fatal("a drag held at the bottom edge asked for no scroll")
		}
		tick = app.update(tick())
	}
	if ansi.Strip(app.View().Content) == before {
		t.Fatal("a drag held at the bottom edge did not scroll the feed")
	}
	released(app, 40, 23)
	if lines := strings.Count(board.written, "\n") + 1; lines <= 14 {
		t.Fatalf("copied %d lines, want more than one screen", lines)
	}
}

func TestAResizeDuringADragCopiesNothing(t *testing.T) {
	board := &stubBoard{}
	app := longAnswerApp(t, board)
	app.update(tea.MouseClickMsg{X: 4, Y: 3, Button: tea.MouseLeft})
	app.update(tea.MouseMotionMsg{X: 20, Y: 8, Button: tea.MouseLeft})
	app.update(tea.WindowSizeMsg{Width: 60, Height: 20})
	released(app, 20, 8)
	if board.writes != 0 {
		t.Fatalf("a resize during a drag still copied %q", board.written)
	}
}
