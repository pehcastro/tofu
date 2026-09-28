package welcome_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui"
)

const (
	width       = 100
	height      = 30
	oldArtLeft  = 35
	artCells    = 30
	wordmarkTop = "▄▄           ▄▄▄▄"
)

func freshApp(t *testing.T, requirements []tui.Requirement) *tui.App {
	t.Setenv("TOFU_ASCII", "")
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("NO_COLOR", "")
	app := tui.New(tui.Options{
		Fresh:        true,
		Pose:         "sitting",
		Keymap:       filepath.Join(t.TempDir(), "keybindings.json"),
		Requirements: requirements,
		Recheck:      func() []tui.Requirement { return nil },
		Turn:         func(context.Context, tui.Pick, string, tui.CalledFromInsideTheTurnAndNeverAfterItReturns) {},
	})
	app.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return app
}

func cells(screen string, rows int) uv.ScreenBuffer {
	buffer := uv.NewScreenBuffer(width, rows)
	uv.NewStyledString(screen).Draw(buffer, uv.Rect(0, 0, width, rows))
	return buffer
}

func column(row, needle string) int {
	at := strings.Index(row, needle)
	if at < 0 {
		return -1
	}
	return utf8.RuneCountInString(row[:at])
}

func wordmarkRow(t *testing.T, screen string) (int, string) {
	t.Helper()
	for index, row := range strings.Split(ansi.Strip(screen), "\n") {
		if strings.Contains(row, wordmarkTop) {
			return index, row
		}
	}
	t.Fatalf("no row holds the top of the wordmark\n%s", ansi.Strip(screen))
	return 0, ""
}

func TestTheChatDrawsTheWordmarkCellForCellAsTheOldCoverDid(t *testing.T) {
	stored, err := os.ReadFile(filepath.Join("testdata", "old-cover-wordmark-sitting-100x30.golden"))
	if err != nil {
		t.Fatal(err)
	}
	old := strings.Split(strings.TrimSuffix(string(stored), "\n"), "\n")
	screen := freshApp(t, nil).View().Content
	top, row := wordmarkRow(t, screen)
	shift := column(row, wordmarkTop) - column(ansi.Strip(old[0]), wordmarkTop)
	want, got := cells(string(stored), len(old)), cells(screen, height)
	for y := range old {
		for x := oldArtLeft; x < oldArtLeft+artCells; x++ {
			was, now := want.CellAt(x, y), got.CellAt(x+shift, top+y)
			if was.Content != now.Content || was.Style != now.Style {
				t.Errorf("wordmark row %d col %d: reference %q %+v, chat %q %+v", y, x, was.Content, was.Style, now.Content, now.Style)
			}
		}
	}
}

func messages(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	switch msg := cmd().(type) {
	case nil:
		return nil
	case tea.BatchMsg:
		var all []tea.Msg
		for _, inner := range msg {
			all = append(all, messages(inner)...)
		}
		return all
	default:
		return []tea.Msg{msg}
	}
}

func dot(t *testing.T, app *tui.App) int {
	_, row := wordmarkRow(t, app.View().Content)
	return column(row, "▀") - column(row, wordmarkTop)
}

func TestOnePulseAfterTheSetupClearsMovesTheDotOneStep(t *testing.T) {
	app := freshApp(t, []tui.Requirement{{What: "sign in", Fix: "tofu login"}})
	initial := app.Init()
	_, recheck := app.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	_, cleared := app.Update(recheck())
	before := dot(t, app)
	for _, msg := range messages(tea.Batch(initial, cleared)) {
		app.Update(msg)
	}
	if moved := dot(t, app) - before; moved != 1 {
		t.Errorf("one pulse interval after the setup cleared moved the dot %d steps", moved)
	}
}
