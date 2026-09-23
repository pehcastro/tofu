package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	composerPrompt = "  "
	composerHeight = 3
)

func cursorApp(t *testing.T) *App {
	t.Helper()
	app := newTestApp(Options{Repo: testRepo, Branch: "develop", Now: fixedClock(), Wires: anthropicAlone})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return app
}

func plainRows(view tea.View) []string {
	rows := strings.Split(view.Content, "\n")
	for index, row := range rows {
		rows[index] = ansi.Strip(row)
	}
	return rows
}

func TestTheSessionCursorSitsInsideTheComposer(t *testing.T) {
	app := cursorApp(t)
	typeText(app, "rename the judge")
	view := app.View()
	if view.Cursor == nil {
		t.Fatal("the session view reports no cursor")
	}
	rows := plainRows(view)
	top := composerTopRow(t, view.Content) + 1
	if view.Cursor.Y < top || view.Cursor.Y >= top+composerHeight {
		t.Fatalf("the cursor is on row %d, want a composer row between %d and %d\n%s",
			view.Cursor.Y, top, top+composerHeight-1, strings.Join(rows, "\n"))
	}
	typed := composerPrompt + "rename the judge"
	if !strings.HasPrefix(rows[view.Cursor.Y], typed) {
		t.Fatalf("the cursor row does not carry what was typed: %q", rows[view.Cursor.Y])
	}
	if view.Cursor.X != len([]rune(typed)) {
		t.Fatalf("the cursor is at column %d on row %q, want %d", view.Cursor.X, rows[view.Cursor.Y], len([]rune(typed)))
	}
}

func TestTheCursorMovesWithTheCaret(t *testing.T) {
	app := cursorApp(t)
	before := app.View().Cursor
	if before == nil {
		t.Fatal("the empty composer reports no cursor")
	}
	typeText(app, "tofu!")
	after := app.View().Cursor
	if after == nil {
		t.Fatal("the typed composer reports no cursor")
	}
	if after.X-before.X != len("tofu!") {
		t.Fatalf("typing 5 characters moved the cursor %d cells, want 5", after.X-before.X)
	}
	if after.Y != before.Y {
		t.Fatalf("typing on one line moved the cursor from row %d to row %d", before.Y, after.Y)
	}
}

func TestANewlineMovesTheCursorDownOneRow(t *testing.T) {
	app := cursorApp(t)
	first := app.View().Cursor
	if first == nil {
		t.Fatal("the empty composer reports no cursor")
	}
	typeText(app, "one")
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift})
	typeText(app, "two")
	view := app.View()
	if view.Cursor == nil {
		t.Fatal("the multi-line composer reports no cursor")
	}
	if view.Cursor.Y != first.Y+1 {
		t.Fatalf("after a newline the cursor is on row %d, want %d", view.Cursor.Y, first.Y+1)
	}
	if view.Cursor.X != len([]rune(composerPrompt+"two")) {
		t.Fatalf("after a newline the cursor is at column %d", view.Cursor.X)
	}
	rows := plainRows(view)
	if !strings.HasPrefix(rows[view.Cursor.Y], composerPrompt+"two") {
		t.Fatalf("the cursor row is %q, want the second composer line", rows[view.Cursor.Y])
	}
}

func TestTheOtherViewsShowNoCursor(t *testing.T) {
	for _, view := range []struct {
		name string
		show func(*App)
	}{
		{"sub-agents", func(app *App) { app.Update(tea.KeyPressMsg{Code: '4', Mod: tea.ModAlt}) }},
		{"settings", func(app *App) { app.runCommand("settings") }},
	} {
		t.Run(view.name, func(t *testing.T) {
			app := cursorApp(t)
			typeText(app, "half a task")
			view.show(app)
			if cursor := app.View().Cursor; cursor != nil {
				t.Fatalf("the %s view puts a cursor at %d,%d", view.name, cursor.X, cursor.Y)
			}
		})
	}
}

func TestTheSetupScreenShowsNoCursor(t *testing.T) {
	setup := newTestApp(Options{Repo: testRepo, Now: fixedClock(), Requirements: setupRequirements(), Wires: anthropicAlone})
	setup.Init()
	setup.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if cursor := setup.View().Cursor; cursor != nil {
		t.Fatalf("the setup screen puts a cursor at %d,%d", cursor.X, cursor.Y)
	}
}
