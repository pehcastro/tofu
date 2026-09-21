package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	isession "tofu/internal/session"
	isettings "tofu/internal/settings"
	"tofu/internal/sys"
)

const (
	readPath = "internal/turn/loop.go"
	readBody = "the whole body of the file he was reading"
)

func corpusApp(t *testing.T) (*App, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := isettings.Open(filepath.Join(dir, "settings.json"), "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	app := newTestApp(Options{Repo: testRepo, Now: fixedClock(), Settings: store, Promotions: isession.NewPromotionLog(dir)})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	app.Update(Event{Kind: EventToolCall, ID: "r1", Tool: "read", Text: readPath})
	app.Update(Event{Kind: EventToolResult, ID: "r1", Text: readBody})
	return app, dir
}

func promotionRows(t *testing.T, dir string) []isession.Promotion {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, isession.PromotionsFileName))
	if err != nil {
		t.Fatalf("no promotion rows were written: %v", err)
	}
	var rows []isession.Promotion
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var row isession.Promotion
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("row %d does not parse: %v", len(rows)+1, err)
		}
		rows = append(rows, row)
	}
	return rows
}

func TestTheRecorderIsOnWhereverTheInterfaceKnowsItsRoot(t *testing.T) {
	root := t.TempDir()
	app := newTestApp(Options{Repo: testRepo, Root: root, Now: fixedClock()})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	app.Update(Event{Kind: EventToolCall, ID: "r1", Tool: "read", Text: readPath})
	app.show(viewWork)
	app.workKey("enter")

	if _, err := os.Stat(filepath.Join(root, sys.StateDirName, isession.PromotionsFileName)); err != nil {
		t.Fatalf("a run that knows its root wrote no promotion log: %v", err)
	}
}

func TestBringingABlockFromWorkIntoChatWritesALabelledRow(t *testing.T) {
	app, dir := corpusApp(t)
	app.show(viewWork)
	app.workKey("enter")

	rows := promotionRows(t, dir)
	if len(rows) != 1 {
		t.Fatalf("reaching into work wrote %d rows, want 1", len(rows))
	}
	row := rows[0]
	if row.Action != isession.ReachedIntoWork {
		t.Errorf("the row is labelled %q, want %q", row.Action, isession.ReachedIntoWork)
	}
	if row.Chose != isession.PlaceChat {
		t.Errorf("the row says he chose %q, want %q", row.Chose, isession.PlaceChat)
	}
	if row.EventID != "r1" || row.EventKind != "read" {
		t.Errorf("the row names event %q of kind %q, want r1 and read", row.EventID, row.EventKind)
	}
	if row.At.IsZero() {
		t.Error("the row carries no time")
	}
}

func TestARowCarriesWhatTheFreeArmDecidedAtTheTime(t *testing.T) {
	folded, dir := corpusApp(t)
	folded.show(viewWork)
	folded.workKey("enter")
	if arm := promotionRows(t, dir)[0].FreeArm; arm != isession.PlaceWork {
		t.Errorf("the free arm folded the call away, and the row says %q, want %q", arm, isession.PlaceWork)
	}

	promoted, promotedDir := corpusApp(t)
	promoted.Update(Event{Kind: EventToolCall, ID: "c1", Tool: "crew", Text: "go-docs: write the docs", Promote: true})
	promoted.Update(Event{Kind: EventToolResult, ID: "c1", Text: "wrote docs/verification.md"})
	promoted.show(viewWork)
	promoted.workKey("down")
	promoted.workKey("enter")
	rows := promotionRows(t, promotedDir)
	if rows[0].EventID != "c1" {
		t.Fatalf("the test reached into %q, not the promoted child", rows[0].EventID)
	}
	if rows[0].FreeArm != isession.PlaceChat {
		t.Errorf("the free arm had already promoted the child, and the row says %q, want %q", rows[0].FreeArm, isession.PlaceChat)
	}
}

func TestMovingAKindBetweenChatAndWorkInSettingsWritesALabelledRow(t *testing.T) {
	app, dir := corpusApp(t)
	app.runCommand("settings")
	app.settingsKey("down")
	app.settingsKey("enter")
	app.settingsKey("enter")

	rows := promotionRows(t, dir)
	if len(rows) != 2 {
		t.Fatalf("two moves of the same kind wrote %d rows, want 2", len(rows))
	}
	for _, row := range rows {
		if row.Action != isession.MovedKind || row.EventKind != toolEventKind {
			t.Fatalf("a settings row is %q on kind %q, want %q on tool", row.Action, row.EventKind, isession.MovedKind)
		}
	}
	if rows[0].FreeArm != isession.PlaceWork || rows[0].Chose != isession.PlaceChat {
		t.Errorf("switching tool calls into chat wrote free arm %q and choice %q", rows[0].FreeArm, rows[0].Chose)
	}
	if rows[1].FreeArm != isession.PlaceChat || rows[1].Chose != isession.PlaceWork {
		t.Errorf("switching tool calls back into work wrote free arm %q and choice %q", rows[1].FreeArm, rows[1].Chose)
	}
}

func TestARowCarriesNoContentFromWorkBeyondTheKindAndTheID(t *testing.T) {
	app, dir := corpusApp(t)
	app.show(viewWork)
	app.workKey("enter")

	raw, err := os.ReadFile(filepath.Join(dir, isession.PromotionsFileName))
	if err != nil {
		t.Fatalf("no promotion rows were written: %v", err)
	}
	for _, content := range []string{readPath, readBody, "loop.go"} {
		if strings.Contains(string(raw), content) {
			t.Errorf("the row carries %q from his work\n%s", content, raw)
		}
	}
	for _, kept := range []string{`"event_id":"r1"`, `"event_kind":"read"`} {
		if !strings.Contains(string(raw), kept) {
			t.Errorf("the row lost %s\n%s", kept, raw)
		}
	}
}
