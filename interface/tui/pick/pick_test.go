package pick

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/theme"
	"tofu/internal/widget"
)

const (
	frameWidth = 40
	marker     = "▌ "
	indent     = "  "
	sentence   = "the gate reads the policy in library/policy/shell.yaml before the wire"
)

func rendered(t *testing.T) []string {
	t.Helper()
	wrapped := widget.Wrap(sentence, frameWidth-widget.Cells(marker))
	if len(wrapped) < 2 {
		t.Fatalf("%q fits on one row at width %d, so nothing is wrapped", sentence, frameWidth)
	}
	rows := make([]string, 0, len(wrapped))
	for index, line := range wrapped {
		prefix := marker
		if index > 0 {
			prefix = indent
		}
		rows = append(rows, widget.Pad(theme.Speech().Render(prefix+line), frameWidth))
	}
	return rows
}

func wholeOf(t *testing.T, rows []string) Selection {
	t.Helper()
	selection := Selection{}
	selection.Begin(Cell{X: 0, Y: 0})
	selection.Extend(Cell{X: frameWidth - 1, Y: len(rows) - 1})
	return selection
}

func TestASelectionAcrossAWrappedLineCopiesTheSentenceRatherThanTheCells(t *testing.T) {
	rows := rendered(t)
	got := wholeOf(t, rows).Text(rows, frameWidth)
	if got != sentence {
		t.Fatalf("the selection copied\n%q\nwant\n%q", got, sentence)
	}
	for _, dropped := range []struct {
		what string
		sign string
	}{
		{"the speech marker drawn at the head of the entry", marker},
		{"the indent drawn on the continuation row", "\n" + indent},
		{"the newline the wrap put between the two halves", "\n"},
		{"the run of two spaces an indent leaves behind", "  "},
	} {
		if strings.Contains(got, dropped.sign) {
			t.Errorf("%s survived the copy: %q is still in %q", dropped.what, dropped.sign, got)
		}
	}
	if strings.HasSuffix(got, " ") {
		t.Errorf("the trailing spaces that pad the row to %d cells survived: %q", frameWidth, got)
	}
	if !strings.Contains(got, "in library/policy/shell.yaml before") {
		t.Errorf("the path did not rejoin its sentence: %q", got)
	}
}

func TestTwoLinesThatWereNeverWrappedKeepTheirNewline(t *testing.T) {
	rows := []string{
		widget.Pad("· the gate is off, so no call on this session is judged.", frameWidth),
		widget.Pad("· cooked for 0s", frameWidth),
	}
	got := wholeOf(t, rows).Text(rows, frameWidth)
	want := "the gate is off, so no call on this session is judged.\ncooked for 0s"
	if got != want {
		t.Fatalf("the selection copied\n%q\nwant\n%q", got, want)
	}
}

func TestDraggingBackwardsCopiesTheSameTextAsDraggingForwards(t *testing.T) {
	rows := rendered(t)
	forwards := wholeOf(t, rows)
	backwards := Selection{}
	backwards.Begin(Cell{X: frameWidth - 1, Y: len(rows) - 1})
	backwards.Extend(Cell{X: 0, Y: 0})
	if forwards.Text(rows, frameWidth) != backwards.Text(rows, frameWidth) {
		t.Errorf("backwards copied %q, forwards copied %q",
			backwards.Text(rows, frameWidth), forwards.Text(rows, frameWidth))
	}
}

func TestPaintCoversOnlyTheSelectedCellsAndKeepsTheRowWidth(t *testing.T) {
	rows := rendered(t)
	selection := Selection{}
	selection.Begin(Cell{X: 2, Y: 0})
	selection.Extend(Cell{X: 10, Y: 0})
	painted := selection.Paint(rows)
	if widget.Cells(painted[0]) != widget.Cells(rows[0]) {
		t.Fatalf("painting changed the row from %d cells to %d", widget.Cells(rows[0]), widget.Cells(painted[0]))
	}
	if ansi.Strip(painted[0]) != ansi.Strip(rows[0]) {
		t.Fatalf("painting changed the text of the row\n%q\n%q", ansi.Strip(painted[0]), ansi.Strip(rows[0]))
	}
	if painted[1] != rows[1] {
		t.Errorf("painting touched a row outside the selection")
	}
	if !strings.Contains(painted[0], ansi.Strip(theme.Selected().Render(""))) && painted[0] == rows[0] {
		t.Errorf("the selected row was not highlighted")
	}
	if painted[0] == rows[0] {
		t.Errorf("the selected row is drawn exactly as it was before the selection")
	}
}

func TestAClearedSelectionPaintsNothingAndCopiesNothing(t *testing.T) {
	rows := rendered(t)
	selection := wholeOf(t, rows)
	selection.Clear()
	if selection.On() {
		t.Fatal("a cleared selection still reports itself active")
	}
	if got := selection.Text(rows, frameWidth); got != "" {
		t.Errorf("a cleared selection copied %q", got)
	}
}
