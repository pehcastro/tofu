package promote

import (
	"errors"
	"io/fs"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	tui "tofu/interface/tui"
	"tofu/internal/session"
	"tofu/internal/settings"
	"tofu/internal/sys"
)

func drive(t *testing.T, dir string) {
	t.Helper()
	store, err := settings.Open(filepath.Join(dir, "settings.json"), "")
	if err != nil {
		t.Fatalf("settings.Open: %v", err)
	}
	at := time.Date(2026, 9, 21, 11, 0, 0, 0, time.UTC)
	app := tui.New(tui.Options{
		Repo:       "bob",
		Release:    "bench",
		Now:        func() time.Time { return at },
		Settings:   store,
		Promotions: session.NewPromotionLog(dir),
	})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	app.Update(tui.Event{Kind: tui.EventToolCall, ID: "r1", Tool: "read", Text: "internal/turn/loop.go"})
	app.Update(tui.Event{Kind: tui.EventToolResult, ID: "r1", Text: "384 lines"})

	app.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	app.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	app.Update(tea.KeyPressMsg{Code: '1', Mod: tea.ModAlt})
	for _, code := range "/settings" {
		app.Update(tea.KeyPressMsg{Code: code, Text: string(code)})
	}
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	app.Update(tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})
	for _, code := range "tool detail" {
		app.Update(tea.KeyPressMsg{Code: code, Text: string(code)})
	}
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	app.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
}

func TestBenchReadsTheRowsTheInterfaceWrote(t *testing.T) {
	dir := t.TempDir()
	drive(t, dir)
	rows, err := Read(dir)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("a reach into work and a kind moved in settings produced %d rows, want 2: %+v", len(rows), rows)
	}
	if rows[0].Action != session.ReachedIntoWork || rows[1].Action != session.MovedKind {
		t.Fatalf("the two rows are %q and %q", rows[0].Action, rows[1].Action)
	}
	t.Log("\n" + Report(rows, dir))
}

func TestTheProjectCorpusIsCountedWhateverItsSize(t *testing.T) {
	dir := sys.RecordedStateDir()
	rows, err := Read(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Read: %v", err)
	}
	if errors.Is(err, fs.ErrNotExist) {
		t.Logf("no promotion log exists yet at %s: 0 rows, %d short of %d", Path(dir), RowsToDecideAnything, RowsToDecideAnything)
		return
	}
	t.Log("\n" + Report(rows, dir))
}
