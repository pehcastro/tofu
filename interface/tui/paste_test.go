package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"boji/interface/tui/paste"
	"boji/internal/sys"
)

const (
	pasteSessionID = "turn-19a2b3c4d5"
	pasteWidth     = 80
	pasteHeight    = 24
	screenshot     = "pretend this is the png encoding of a screenshot"
)

func pasteApp(t *testing.T, board paste.Board) *App {
	t.Helper()
	at := time.Date(2026, 9, 19, 14, 32, 0, 0, time.UTC)
	app := newTestApp(Options{
		Repo:   "silo",
		Branch: "develop",
		Wires:  anthropicAlone,
		Now:    func() time.Time { return at },
		Paste:  board,
	})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: pasteWidth, Height: pasteHeight})
	return app
}

func pasteBoard(t *testing.T) (paste.Board, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), pasteSessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("the session directory could not be made: %v", err)
	}
	return paste.Board{
		Read: func() (sys.Clipboard, error) {
			return sys.Clipboard{Kind: sys.ClipboardImage, PNG: []byte(screenshot)}, nil
		},
		Dir: func() (string, error) { return dir, nil },
	}, dir
}

func pasteKeys() map[string]tea.KeyPressMsg {
	return map[string]tea.KeyPressMsg{
		"ctrl+v": {Code: 'v', Mod: tea.ModCtrl},
		"alt+v":  {Code: 'v', Mod: tea.ModAlt},
	}
}

func TestEitherPasteKeyAttachesAnImage(t *testing.T) {
	for name, key := range pasteKeys() {
		t.Run(name, func(t *testing.T) {
			board, dir := pasteBoard(t)
			app := pasteApp(t, board)
			_, cmd := app.Update(key)
			if cmd == nil {
				t.Fatalf("%s returned no command, so nothing was pasted", name)
			}
			if !strings.Contains(ansi.Strip(app.View().Content), "image 1  pasting") {
				t.Fatalf("%s left no placeholder in the composer:\n%s", name, ansi.Strip(app.View().Content))
			}
			app.Update(cmd())
			frame := ansi.Strip(app.View().Content)
			if !strings.Contains(frame, pasteSessionID+"-image-01.png") {
				t.Fatalf("%s did not name the written file:\n%s", name, frame)
			}
			if _, err := os.Stat(filepath.Join(dir, pasteSessionID+"-image-01.png")); err != nil {
				t.Fatalf("%s wrote nothing beside the record: %v", name, err)
			}
		})
	}
}

func TestThePasteKeypressReturnsBeforeTheImageIsWritten(t *testing.T) {
	board, _ := pasteBoard(t)
	release := make(chan struct{})
	board.Write = func(string, []byte) error {
		<-release
		return nil
	}
	app := pasteApp(t, board)
	_, cmd := app.Update(tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl})
	written := make(chan tea.Msg, 1)
	go func() { written <- cmd() }()

	frame := ansi.Strip(app.View().Content)
	if !strings.Contains(frame, "image 1  pasting") {
		t.Fatalf("the frame does not render while the writer is blocked:\n%s", frame)
	}
	select {
	case msg := <-written:
		t.Fatalf("the write finished before it was released, returning %v", msg)
	default:
	}
	close(release)
	app.Update(<-written)
	if !strings.Contains(ansi.Strip(app.View().Content), "-image-01.png") {
		t.Fatal("the released write never reached the composer")
	}
}

func TestAFailedPasteKeepsThePlaceholderAndSaysWhy(t *testing.T) {
	board, _ := pasteBoard(t)
	board.Write = func(string, []byte) error { return errors.New("the disk is full") }
	app := pasteApp(t, board)
	_, cmd := app.Update(tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl})
	app.Update(cmd())
	frame := ansi.Strip(app.View().Content)
	for _, want := range []string{"image 1", "could not be pasted: the disk is full"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("the frame does not say %q:\n%s", want, frame)
		}
	}
}

func TestPastingTextLeavesNoPlaceholderAndTypesTheText(t *testing.T) {
	board, _ := pasteBoard(t)
	board.Read = func() (sys.Clipboard, error) {
		return sys.Clipboard{Kind: sys.ClipboardText, Text: "rerun the gate bench"}, nil
	}
	app := pasteApp(t, board)
	_, cmd := app.Update(tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl})
	app.Update(cmd())
	frame := ansi.Strip(app.View().Content)
	if strings.Contains(frame, "image 1") {
		t.Fatalf("text left an image placeholder behind:\n%s", frame)
	}
	if !strings.Contains(frame, "rerun the gate bench") {
		t.Fatalf("the pasted text is not in the composer:\n%s", frame)
	}
}

func TestThePastedComposerIsWhatHeSees(t *testing.T) {
	board, _ := pasteBoard(t)
	app := pasteApp(t, board)
	_, cmd := app.Update(tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl})
	app.Update(cmd())
	typeText(app, "what changed between these two frames?")
	rows := strings.Split(ansi.Strip(app.View().Content), "\n")
	composer := strings.Join(rows[len(rows)-6:], "\n")
	if !strings.Contains(composer, "what changed between these two frames?") {
		t.Fatalf("the composer does not hold the typed task:\n%s", composer)
	}
	t.Log("\n" + composer)
}
