package tui

import (
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/fixture"
	"tofu/interface/tui/frametime"
	"tofu/interface/tui/pick"
	"tofu/internal/widget"
)

const wrappedNote = "the gate reads the policy in catalog/policy/shell.yaml before it reaches " +
	"the wire, and the wire declares its own limits rather than the caller guessing them"

var (
	noteHead = pick.Cell{X: 2, Y: 3}
	noteTail = pick.Cell{X: 5, Y: 5}
)

func mouseApp(t *testing.T, notes []string) (*App, func() []string) {
	t.Helper()
	var written []string
	app := newTestApp(Options{
		Repo: testRepo, Branch: "develop", Now: fixedClock(), Wires: bothWires,
		Copy: func(text string) error {
			written = append(written, text)
			return nil
		},
	})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	app.Update(fixture.Quotas(fixedClock()()))
	for _, note := range notes {
		app.Update(Event{Kind: EventNote, Text: note})
	}
	app.View()
	return app, func() []string { return written }
}

func drag(app *App, from, to pick.Cell) {
	app.Update(tea.MouseClickMsg{X: from.X, Y: from.Y, Button: tea.MouseLeft})
	app.Update(tea.MouseMotionMsg{X: to.X, Y: to.Y, Button: tea.MouseLeft})
}

func click(app *App, at pick.Cell) {
	app.Update(tea.MouseClickMsg{X: at.X, Y: at.Y, Button: tea.MouseLeft})
	app.Update(tea.MouseReleaseMsg{X: at.X, Y: at.Y, Button: tea.MouseLeft})
}

func TestAClickOnATabSwitchesViewWhileTheMouseIsReported(t *testing.T) {
	app := sessionApp(t, 80, 24)
	mode := app.View().MouseMode
	if mode != tea.MouseModeCellMotion && mode != tea.MouseModeAllMotion {
		t.Fatalf("the view reports mouse mode %v, so no click ever reaches Update", mode)
	}
	column, hit := stripColumn(app, "[5] shells")
	if !hit {
		t.Fatal("the strip registered no zone for the shells view")
	}
	click(app, pick.Cell{X: column, Y: stripRow})
	if app.current != viewShells {
		t.Fatalf("a click at column %d reached view %d, want the shells view", column, app.current)
	}
}

func TestAClickOnATraceIDOpensThatEventInWork(t *testing.T) {
	app := sessionApp(t, 80, 24)
	app.View()
	column, row := -1, -1
	for index, line := range app.frame {
		plain := ansi.Strip(line)
		if offset := strings.Index(plain, "[#c1]"); offset >= 0 {
			column, row = widget.Cells(plain[:offset])+1, index
			break
		}
	}
	if row < 0 {
		t.Fatal("no trace id is drawn on the chat view")
	}
	click(app, pick.Cell{X: column, Y: row})
	if app.current != viewWork {
		t.Fatalf("a click on the id at %d,%d reached view %d, want work", column, row, app.current)
	}
}

func TestADragOverTheTranscriptHighlightsTheCoveredText(t *testing.T) {
	app, _ := mouseApp(t, []string{wrappedNote})
	quiet := app.View().Content
	drag(app, noteHead, noteTail)
	painted := app.View().Content
	if painted == quiet {
		t.Fatal("the frame mid-drag is drawn exactly as the frame before the drag")
	}
	if ansi.Strip(painted) != ansi.Strip(quiet) {
		t.Fatal("the highlight changed the text of the frame rather than only its colour")
	}
	assertGolden(t, "drag-mid-80x24.golden", painted)
}

func TestReleasingTheButtonPutsTheSelectionOnTheClipboard(t *testing.T) {
	app, written := mouseApp(t, []string{wrappedNote})
	drag(app, noteHead, noteTail)
	_, cmd := app.Update(tea.MouseReleaseMsg{X: noteTail.X, Y: noteTail.Y, Button: tea.MouseLeft})
	if cmd == nil {
		t.Fatal("releasing the button asked for no work, so nothing was copied")
	}
	cmd()
	copied := written()
	if len(copied) != 1 {
		t.Fatalf("the clipboard was written %d times, want once", len(copied))
	}
	if copied[0] != wrappedNote {
		t.Fatalf("the clipboard holds\n%q\nwant\n%q", copied[0], wrappedNote)
	}
}

func TestASelectionThatCrossesAScrollKeepsWhatWasAlreadySelected(t *testing.T) {
	notes := make([]string, 0, 40)
	for line := range 40 {
		notes = append(notes, "note "+strconv.Itoa(line)+" about the gate and the policy it reads")
	}
	app, written := mouseApp(t, notes)
	drag(app, pick.Cell{X: 2, Y: 3}, pick.Cell{X: 40, Y: 4})
	before := app.selection.Text(app.frame, app.width)
	if before == "" {
		t.Fatal("the drag selected nothing")
	}
	app.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	app.View()
	if after := app.selection.Text(app.frame, app.width); after != before {
		t.Fatalf("the scroll moved the selection off its text\n--- after ---\n%q\n--- before ---\n%q", after, before)
	}
	_, cmd := app.Update(tea.MouseReleaseMsg{X: 40, Y: 7, Button: tea.MouseLeft})
	if cmd == nil {
		t.Fatal("releasing after a scroll copied nothing")
	}
	cmd()
	if copied := written(); len(copied) != 1 || copied[0] != before {
		t.Fatalf("the clipboard holds %q, want %q", copied, before)
	}
}

func TestAClickInsideAClickableRegionDoesNotStartASelection(t *testing.T) {
	app, _ := mouseApp(t, []string{wrappedNote})
	column, hit := stripColumn(app, "[2] work")
	if !hit {
		t.Fatal("the strip registered no zone for the work view")
	}
	click(app, pick.Cell{X: column, Y: stripRow})
	if app.selection.On() {
		t.Fatal("a click on a tab began a selection")
	}
	if app.current != viewWork {
		t.Fatalf("the click reached view %d, want work", app.current)
	}
}

func TestADragThatStartsInsideAClickableRegionSelectsAndCancelsTheSwitch(t *testing.T) {
	app, _ := mouseApp(t, []string{wrappedNote})
	column, hit := stripColumn(app, "[2] work")
	if !hit {
		t.Fatal("the strip registered no zone for the work view")
	}
	drag(app, pick.Cell{X: column, Y: stripRow}, pick.Cell{X: column + 10, Y: stripRow})
	if !app.selection.On() {
		t.Fatal("a drag that began on a tab selected nothing")
	}
	if text := app.selection.Text(app.frame, app.width); text == "" {
		t.Error("the drag that began on a tab copied an empty string")
	}
	app.Update(tea.MouseReleaseMsg{X: column + 10, Y: stripRow, Button: tea.MouseLeft})
	if app.current != viewChat {
		t.Fatalf("the drag across the strip switched to view %d, want the chat view it began on", app.current)
	}
}

func TestTheFrameBudgetHoldsWithASelectionDrawn(t *testing.T) {
	app, _ := mouseApp(t, []string{wrappedNote})
	drag(app, noteHead, pick.Cell{X: 60, Y: 17})
	frametime.Frames(t, "a frame drawn with a selection over fifteen rows", func() { app.View() })
}
