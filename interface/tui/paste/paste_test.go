package paste

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"boji/internal/sys"
)

const (
	sessionID  = "turn-19a2b3c4d5"
	headerName = "header.json"
	bitmap     = "pretend this is the png encoding of a screenshot"
	frameWidth = 80
	fileMode   = 0o644
	dirMode    = 0o755
)

func sessionDirectory(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), sessionID)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		t.Fatalf("the session directory could not be made: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, headerName), []byte(`{"id":"`+sessionID+`"}`), fileMode); err != nil {
		t.Fatalf("the session record could not be written: %v", err)
	}
	return dir
}

func board(t *testing.T, dir string, clip sys.Clipboard) Board {
	t.Helper()
	return Default(Board{
		Read: func() (sys.Clipboard, error) { return clip, nil },
		Dir:  func() (string, error) { return dir, nil },
	})
}

func attached(t *testing.T, board Board, index int) Outcome {
	t.Helper()
	msg := board.Attach(index)()
	outcome, ok := msg.(Outcome)
	if !ok {
		t.Fatalf("attaching returned %T, want an Outcome", msg)
	}
	return outcome
}

func TestABitmapOnTheClipboardBecomesAFileAndALine(t *testing.T) {
	dir := sessionDirectory(t)
	outcome := attached(t, board(t, dir, sys.Clipboard{Kind: sys.ClipboardImage, PNG: []byte(bitmap)}), 1)
	if outcome.State != Ready {
		t.Fatalf("the paste is in state %d with cause %q, want Ready", outcome.State, outcome.Cause)
	}
	body, err := os.ReadFile(filepath.Join(dir, outcome.Name))
	if err != nil {
		t.Fatalf("the pasted image is not on disk: %v", err)
	}
	if string(body) != bitmap {
		t.Fatalf("the file holds %q, want the clipboard bytes", body)
	}
	line := ansi.Strip(outcome.Render(frameWidth))
	for _, want := range []string{"image 1", "PNG", "48 bytes", outcome.Name} {
		if !strings.Contains(line, want) {
			t.Errorf("the composer line %q does not name %q", line, want)
		}
	}
}

func TestAFilePathOnTheClipboardIsCopiedWithoutReEncoding(t *testing.T) {
	dir := sessionDirectory(t)
	source := filepath.Join(t.TempDir(), "screenshot.jpg")
	original := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F'}
	if err := os.WriteFile(source, original, fileMode); err != nil {
		t.Fatalf("the source image could not be written: %v", err)
	}
	outcome := attached(t, board(t, dir, sys.Clipboard{Kind: sys.ClipboardFiles, Files: []string{source}}), 1)
	if outcome.State != Ready {
		t.Fatalf("the paste is in state %d with cause %q, want Ready", outcome.State, outcome.Cause)
	}
	if filepath.Ext(outcome.Name) != ".jpg" {
		t.Fatalf("the copy is named %q, and a jpeg must stay a jpeg", outcome.Name)
	}
	copied, err := os.ReadFile(filepath.Join(dir, outcome.Name))
	if err != nil {
		t.Fatalf("the copied image is not on disk: %v", err)
	}
	if string(copied) != string(original) {
		t.Fatalf("the copy holds %v, want the original bytes %v", copied, original)
	}
	if !strings.Contains(ansi.Strip(outcome.Render(frameWidth)), "JPG") {
		t.Errorf("the composer line does not name the format: %q", ansi.Strip(outcome.Render(frameWidth)))
	}
}

func TestTextOnTheClipboardPastesAsText(t *testing.T) {
	dir := sessionDirectory(t)
	outcome := attached(t, board(t, dir, sys.Clipboard{Kind: sys.ClipboardText, Text: "go test ./internal/sys/..."}), 1)
	if outcome.State != Textual || outcome.Text != "go test ./internal/sys/..." {
		t.Fatalf("text came back as state %d with text %q", outcome.State, outcome.Text)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("the session directory is unreadable: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("pasting text left %d files beside the record, want only the record", len(entries))
	}
}

func TestEachPastedImageLandsBesideTheRecordInOrder(t *testing.T) {
	dir := sessionDirectory(t)
	made := board(t, dir, sys.Clipboard{Kind: sys.ClipboardImage, PNG: []byte(bitmap)})
	attached(t, made, 1)
	attached(t, made, 2)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("the session directory is unreadable: %v", err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	want := []string{headerName, sessionID + "-image-01.png", sessionID + "-image-02.png"}
	if strings.Join(names, " ") != strings.Join(want, " ") {
		t.Fatalf("the session directory holds %v, want %v", names, want)
	}
}

func TestAFailedWriteSaysSoAndKeepsThePlaceholder(t *testing.T) {
	dir := sessionDirectory(t)
	made := board(t, dir, sys.Clipboard{Kind: sys.ClipboardImage, PNG: []byte(bitmap)})
	made.Write = func(string, []byte) error { return errors.New("the disk is full") }
	outcome := attached(t, made, 3)
	if outcome.State != Failed {
		t.Fatalf("a refused write came back in state %d, want Failed", outcome.State)
	}
	line := ansi.Strip(outcome.Render(frameWidth))
	for _, want := range []string{"image 3", "the disk is full"} {
		if !strings.Contains(line, want) {
			t.Errorf("the line %q does not name %q", line, want)
		}
	}
}

func TestAFailedClipboardReadSaysSo(t *testing.T) {
	held := errors.New("another program is holding the clipboard")
	made := Default(Board{
		Read: func() (sys.Clipboard, error) { return sys.Clipboard{}, held },
		Dir:  func() (string, error) { return sessionDirectory(t), nil },
	})
	outcome := attached(t, made, 1)
	if outcome.State != Failed || !strings.Contains(outcome.Cause, "holding the clipboard") {
		t.Fatalf("a refused read came back in state %d with cause %q", outcome.State, outcome.Cause)
	}
}

func TestAClipboardFileThatIsNotAnImageIsRefused(t *testing.T) {
	dir := sessionDirectory(t)
	source := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(source, []byte("nothing"), fileMode); err != nil {
		t.Fatalf("the source file could not be written: %v", err)
	}
	outcome := attached(t, board(t, dir, sys.Clipboard{Kind: sys.ClipboardFiles, Files: []string{source}}), 1)
	if outcome.State != Failed {
		t.Fatalf("a text file attached as an image, in state %d", outcome.State)
	}
}
