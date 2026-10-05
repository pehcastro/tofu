package hostkeys

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"tofu/internal/golden"
	"tofu/internal/keymap"
)

var (
	enter     = tea.KeyPressMsg{Code: tea.KeyEnter}
	up        = tea.KeyPressMsg{Code: tea.KeyUp}
	down      = tea.KeyPressMsg{Code: tea.KeyDown}
	backspace = tea.KeyPressMsg{Code: tea.KeyBackspace}
)

func ctrl(letter rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: letter, Mod: tea.ModCtrl} }

func tempKeymap(t *testing.T, program string, content string) string {
	t.Helper()
	home := t.TempDir()
	for _, name := range []string{"APPDATA", "USERPROFILE", "HOME", "XDG_CONFIG_HOME"} {
		t.Setenv(name, home)
	}
	t.Setenv("TERM_PROGRAM", program)
	t.Setenv("ZED_TERM", "")
	path, err := keymap.KeymapPath(keymap.DetectHost().Name)
	if err != nil || !strings.HasPrefix(path, home) {
		t.Fatalf("keymap path %q is not under the temp home %q: %v", path, home, err)
	}
	if content == "" {
		return path
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func blank(width, height int) string {
	return strings.TrimSuffix(strings.Repeat(strings.Repeat(" ", width)+"\n", height), "\n")
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestTofuKeybindingsApplyAndUndoFromSettings(t *testing.T) {
	original := "[\n  " + strings.Repeat("/", 2) + " keep this comment\n  {\"context\": \"Workspace\", \"bindings\": {}},\n]\n"
	path := tempKeymap(t, "zed", original)
	t.Logf("before:\n%s", original)
	h := NewHost(keymap.DefaultShortcuts())
	view := ansi.Strip(h.Over(blank(100, 30), 100, 30))
	for _, item := range []string{"CURRENT HOST", "ZED USER KEYMAP", "Apply tofu keybindings", "Undo tofu keybindings"} {
		if !strings.Contains(view, item) {
			t.Fatalf("setup dialog missing %q", item)
		}
	}
	if cmd, closed := h.Key(enter); cmd != nil || closed || h.pending != apply {
		t.Fatal("apply choice did not open confirmation")
	}
	confirm := h.Over(blank(60, 20), 60, 20)
	if lipgloss.Width(confirm) > 60 || lipgloss.Height(confirm) > 20 || !strings.Contains(ansi.Strip(confirm), "Apply to Zed") {
		t.Fatal("narrow confirmation is clipped")
	}
	if read(t, path) != original {
		t.Fatal("opening confirmation edited keymap")
	}
	h.Key(up)
	cmd, _ := h.Key(enter)
	if cmd == nil || !h.busy {
		t.Fatal("confirming apply did not start operation")
	}
	if !h.Result(cmd()) || h.status != string(keymap.Managed) || len(h.backups) == 0 {
		t.Fatalf("apply did not complete: %q, %q", h.status, h.feedback)
	}
	t.Logf("after apply:\n%s", read(t, path))
	h.Key(down)
	h.Key(enter)
	if h.pending != undo {
		t.Fatal("undo choice did not open confirmation")
	}
	h.Key(up)
	cmd, _ = h.Key(enter)
	h.Result(cmd())
	restored := read(t, path)
	t.Logf("after undo:\n%s", restored)
	if restored != original || h.status != string(keymap.NotConfigured) {
		t.Fatalf("undo did not restore keymap: %q", h.status)
	}
}

func TestHostIntegrationClickIsBoundedToItsChoice(t *testing.T) {
	tempKeymap(t, "zed", "")
	h := NewHost(keymap.DefaultShortcuts())
	if cmd, closed := h.Click(1, 1, 80, 24); cmd != nil || closed || h.pending != "" {
		t.Fatal("outside click activated host integration")
	}
	for y, line := range strings.Split(ansi.Strip(h.Over(blank(80, 24), 80, 24)), "\n") {
		if x := strings.Index(line, "Apply tofu keybindings"); x >= 0 {
			h.Click(ansi.StringWidth(line[:x]), y, 80, 24)
			if h.pending != apply {
				t.Fatal("clicking apply label did not open confirmation")
			}
			return
		}
	}
	t.Fatal("apply choice was not visible")
}

func TestVSCodeHostIntegrationAppliesOnlyAfterConfirmation(t *testing.T) {
	original := "[]\n"
	path := tempKeymap(t, "vscode", original)
	h := NewHost(keymap.DefaultShortcuts())
	view := ansi.Strip(h.Over(blank(100, 30), 100, 30))
	for _, want := range []string{"VS Code", "SHORTCUT CONFLICTS", "VS Code chord prefix", "Apply tofu keybindings"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q", want)
		}
	}
	if cmd, _ := h.Key(enter); cmd != nil || h.pending != apply {
		t.Fatal("VS Code apply did not open confirmation")
	}
	if read(t, path) != original {
		t.Fatal("changed keymap before confirmation")
	}
	h.Key(up)
	cmd, _ := h.Key(enter)
	if cmd == nil {
		t.Fatal("confirmed VS Code apply did not run")
	}
	h.Result(cmd())
	if h.status != string(keymap.Managed) {
		t.Fatalf("VS Code binding not managed: %s", h.status)
	}
	current := read(t, path)
	if !strings.Contains(current, "terminalFocus") {
		t.Fatal("missing terminal-only binding")
	}
	for _, key := range []string{`"key":"ctrl+k"`, `"key":"alt+k"`, `"key":"ctrl+l"`, `"key":"ctrl+r"`} {
		if !strings.Contains(current, key) {
			t.Fatalf("configured tofu shortcut %s not forwarded", key)
		}
	}
}

func TestSupportedHostDialogsFitCommonSizes(t *testing.T) {
	for _, host := range []string{"zed", "vscode"} {
		t.Run(host, func(t *testing.T) {
			tempKeymap(t, host, "")
			h := NewHost(keymap.DefaultShortcuts())
			for _, size := range [][2]int{{60, 20}, {80, 24}, {104, 32}, {168, 42}} {
				view := h.Over(blank(size[0], size[1]), size[0], size[1])
				if lipgloss.Height(view) > size[1] || lipgloss.Width(view) > size[0] {
					t.Fatalf("%s dialog overflows %dx%d", host, size[0], size[1])
				}
			}
		})
	}
}

func TestShortcutEditorCapturesPersistsAndRoutes(t *testing.T) {
	tempKeymap(t, "zed", "")
	path := filepath.Join(t.TempDir(), "keybindings.json")
	s := NewShortcuts(path, keymap.DefaultShortcuts())
	if !strings.Contains(ansi.Strip(s.Over(blank(80, 24), 80, 24)), "Search") {
		t.Fatal("shortcut list is not visible")
	}
	s.Key(enter)
	if s.capture != "Search" {
		t.Fatal("search row did not enter capture mode")
	}
	if changed, closed := s.Key(ctrl('n')); !changed || closed || s.Bindings()["Search"] != "ctrl+n" {
		t.Fatalf("captured shortcut: %q", s.Bindings()["Search"])
	}
	if !strings.Contains(s.feedback, "; check Host integration") {
		t.Fatal("shortcut change did not signal host reapply")
	}
	if got := keymap.LoadShortcuts(path)["Search"]; got != "ctrl+n" {
		t.Fatalf("persisted shortcut: %q", got)
	}
	s.Key(down)
	s.Key(enter)
	if changed, _ := s.Key(ctrl('n')); changed || !strings.Contains(s.feedback, "already assigned to Search") || s.Bindings()["Commands"] != "alt+k" {
		t.Fatal("collision was not rejected")
	}
	s.Key(enter)
	if changed, _ := s.Key(ctrl('c')); changed || !strings.Contains(s.feedback, "reserved") || s.Bindings()["Commands"] != "alt+k" {
		t.Fatal("reserved quit key was not rejected")
	}
	s.Key(enter)
	if s.Key(tea.KeyPressMsg{Code: tea.KeyEscape}); s.feedback != "Change cancelled" || s.capture != "" {
		t.Fatal("escape did not cancel the capture")
	}
	s.Key(up)
	if changed, _ := s.Key(backspace); !changed || s.Bindings()["Search"] != "" || keymap.LoadShortcuts(path)["Search"] != "" {
		t.Fatal("clearing shortcut did not persist")
	}
}

func TestShortcutFileRejectsDuplicateBindings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keybindings.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"bindings":{"Search":"alt+k"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	bindings := NewShortcuts(path, keymap.LoadShortcuts(path)).Bindings()
	if bindings["Search"] != "ctrl+k" || bindings["Commands"] != "alt+k" {
		t.Fatal("duplicate saved binding was accepted")
	}
}

func TestASavedCtrlGLeavesTheEditorUnbound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keybindings.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"bindings":{"Search":"ctrl+g"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	want := keymap.DefaultShortcuts()
	want["Search"], want[keymap.EditorAction] = "ctrl+g", ""
	if got := NewShortcuts(path, keymap.LoadShortcuts(path)).Bindings(); !maps.Equal(got, want) {
		t.Fatalf("a file binding Search to ctrl+g loaded as %v, want %v", got, want)
	}
}

func TestDialogGoldens(t *testing.T) {
	tempKeymap(t, "zed", "[]\n")
	h := NewHost(keymap.DefaultShortcuts())
	golden.Assert(t, "host-zed-100x30.golden", ansi.Strip(h.Over(blank(100, 30), 100, 30)))
	golden.Assert(t, "host-zed-60x20.golden", ansi.Strip(h.Over(blank(60, 20), 60, 20)))
	s := NewShortcuts(filepath.Join(t.TempDir(), "keybindings.json"), keymap.DefaultShortcuts())
	s.Key(down)
	s.Key(enter)
	golden.Assert(t, "keybindings-capturing-80x24.golden", ansi.Strip(s.Over(blank(80, 24), 80, 24)))
}
