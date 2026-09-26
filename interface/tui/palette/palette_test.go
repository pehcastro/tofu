package palette

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"tofu/internal/golden"
)

const (
	testWidth  = 120
	testHeight = 36
)

func testBase() string {
	rows := make([]string, testHeight)
	for y := range rows {
		rows[y] = strings.Repeat("underlay ", testWidth)[:testWidth]
	}
	return strings.Join(rows, "\n")
}

func testCommands() Commands {
	return NewCommands([]Item{
		{Title: "New session", Description: "Start with a clean conversation", Key: "ctrl+n", ID: "new"},
		{Title: "Switch model", Description: "Choose the model for this session", Key: "ctrl+l", ID: "model"},
		{Title: "Attach file", Description: "Insert a workspace reference", Key: "@", ID: "attach"},
		{Title: "Settings", Description: "Change how tofu looks and behaves", Key: "/settings", ID: "settings"},
		{Title: "Quit tofu", Description: "Return to the shell", Key: "ctrl+c", ID: "quit"},
	})
}

func testScreens() []Result {
	return []Result{
		{Label: "Chat", Detail: "Conversation and composer", Screen: 0},
		{Label: "Sub-agents", Detail: "Delegated work", Screen: 1},
		{Label: "File edits", Detail: "Changes and diffs", Screen: 2},
		{Label: "Shells", Detail: "Processes and output", Screen: 3},
		{Label: "Settings", Detail: "Appearance and behaviour", Screen: 4},
	}
}

func testFind(query string) []Result {
	if query == "" {
		return testScreens()
	}
	var found []Result
	for _, result := range append(testScreens(), Result{Label: "Shells / Stop on exit", Detail: "End background shells with tofu", Screen: 4, Section: 2, Row: 1}) {
		if strings.Contains(strings.ToLower(result.Label+" "+result.Detail), query) {
			found = append(found, result)
		}
	}
	return found
}

func typed(text string) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: []rune(text)[0], Text: text}
}

func TestModalFilterAcceptsText(t *testing.T) {
	c := testCommands()
	c.Key(typed("m"))
	if got := c.list.input.Value(); got != "m" {
		t.Fatalf("modal filter did not receive text: %q", got)
	}
}

func TestCommandFilterChangesRenderedRows(t *testing.T) {
	c := testCommands()
	for _, r := range "model" {
		c.Key(typed(string(r)))
	}
	view := c.Over(testBase(), 100, 30)
	if !strings.Contains(view, "Switch model") || strings.Contains(view, "New session") {
		t.Fatal("command filter did not narrow rendered results")
	}
}

func TestCommandPaletteIsCentered(t *testing.T) {
	c := testCommands()
	for _, line := range strings.Split(ansi.Strip(c.Over(testBase(), testWidth, testHeight)), "\n") {
		if at := strings.Index(line, "Commands"); at >= 0 {
			if at < 20 || at > 45 {
				t.Fatalf("palette title starts at column %d", at)
			}
			return
		}
	}
	t.Fatal("palette title was not rendered")
}

func TestGlobalSearchClickDoesNotLeakPastResult(t *testing.T) {
	s := NewSearch("Search", "Find views, agents, events, and settings", testFind)
	for y, line := range strings.Split(ansi.Strip(s.Over(testBase(), testWidth, testHeight)), "\n") {
		at := strings.Index(line, "File edits")
		if at < 0 {
			continue
		}
		x := lipgloss.Width(line[:at])
		if outside := s.Click(x-1, y, testWidth, testHeight); outside.Done || !outside.Inside {
			t.Fatalf("a click outside the search label escaped the dialog: %+v", outside)
		}
		if chosen := s.Click(x, y, testWidth, testHeight); !chosen.Done || chosen.Result.Label != "File edits" {
			t.Fatalf("search result click missed its destination: %+v", chosen)
		}
		return
	}
	t.Fatal("File edits result was not visible")
}

func TestFileReferencePickerStaysInWorkspace(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "activity.go"), []byte("package root\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, cmd := NewFiles(root)
	if cmd == nil {
		t.Fatal("file picker did not request directory contents")
	}
	f.Update(cmd())
	if !strings.Contains(f.picker.View(), "activity.go") {
		t.Fatalf("workspace files did not load: picker=%q", ansi.Strip(f.picker.View()))
	}
	choice, _ := f.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	if f.picker.CurrentDirectory != root || choice.Notice != outsideWorkspace {
		t.Fatalf("file picker escaped workspace root: dir=%q notice=%q", f.picker.CurrentDirectory, choice.Notice)
	}
}

func TestGoldens(t *testing.T) {
	commands := testCommands()
	for _, r := range "model" {
		commands.Key(typed(string(r)))
	}
	golden.Assert(t, "commands-model-120x36.golden", ansi.Strip(commands.Over(testBase(), testWidth, testHeight)))

	search := NewSearch("Search", "Find views, agents, events, and settings", testFind)
	golden.Assert(t, "search-empty-120x36.golden", ansi.Strip(search.Over(testBase(), testWidth, testHeight)))
	for _, r := range "shell" {
		search.Key(typed(string(r)))
	}
	golden.Assert(t, "search-shell-120x36.golden", ansi.Strip(search.Over(testBase(), testWidth, testHeight)))

	confirm := NewConfirm("Run shell command?", "This action leaves the current read-only step.", "go test ./interface/tui/...", []Item{
		{Title: "Allow once", Description: "Run this command only", ID: "once"},
		{Title: "Allow for session", Description: "Remember until tofu exits", ID: "session"},
		{Title: "Deny", Description: "Return without running it", ID: "deny"},
	})
	golden.Assert(t, "confirm-120x36.golden", ansi.Strip(confirm.Over(testBase(), testWidth, testHeight)))

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "internal"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module root\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, cmd := NewFiles(root)
	files.Update(cmd())
	golden.Assert(t, "files-120x36.golden", ansi.Strip(files.Over(testBase(), testWidth, testHeight)))
}

func BenchmarkCommandsOver(b *testing.B) {
	c, base := testCommands(), testBase()
	for b.Loop() {
		c.Over(base, testWidth, testHeight)
	}
}

func BenchmarkSearchOver(b *testing.B) {
	s, base := NewSearch("Search", "Find views, agents, events, and settings", testFind), testBase()
	for b.Loop() {
		s.Over(base, testWidth, testHeight)
	}
}
