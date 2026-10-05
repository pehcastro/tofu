package keymap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfiguredShortcutsDriveHostPlan(t *testing.T) {
	shortcuts := DefaultShortcuts()
	shortcuts["Search"] = "ctrl+n"
	plan, skipped := Plan(VSCode, shortcuts, nil)
	if len(skipped) != 0 || len(plan) != 5 || plan[0].Key != "ctrl+n" {
		t.Fatalf("plan did not follow tofu settings: %+v, %v", plan, skipped)
	}
	for _, element := range elements(VSCode, shortcuts) {
		if strings.Contains(element, `"key":"ctrl+k"`) {
			t.Fatal("hard-coded Ctrl+K remained after remap")
		}
	}
}

func TestHostPreviewExplainsExistingUserShortcut(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keybindings.json")
	original := `[{"key":"alt+k","command":"extension.otherAction","when":"terminalFocus"}]`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	shortcuts := DefaultShortcuts()
	plan, _ := Plan(VSCode, shortcuts, Collisions(VSCode, path, shortcuts))
	explained := false
	for _, item := range plan {
		explained = explained || item.Reason == "takes priority over user rule: extension.otherAction"
	}
	if !explained {
		t.Fatalf("preview did not explain overridden user shortcut: %+v", plan)
	}
	if _, _, err := Apply(VSCode, path, shortcuts); err != nil {
		t.Fatal(err)
	}
	current, _ := os.ReadFile(path)
	if strings.Index(string(current), "extension.otherAction") >= strings.LastIndex(string(current), "terminal.sendSequence") {
		t.Fatal("tofu rule is not later than conflicting user rule")
	}
	if _, _, err := Undo(VSCode, path); err != nil {
		t.Fatal(err)
	}
	restored, _ := os.ReadFile(path)
	if string(restored) != original {
		t.Fatal("undo did not restore user rule exactly")
	}
}

func installedCopy(t *testing.T, host HostName) (live, copied string, original []byte) {
	t.Helper()
	live, err := KeymapPath(host)
	if err != nil {
		t.Skip(err)
	}
	original, err = os.ReadFile(live)
	if os.IsNotExist(err) {
		t.Skipf("%s keymap does not exist", host)
	}
	if err != nil {
		t.Fatal(err)
	}
	copied = filepath.Join(t.TempDir(), filepath.Base(live))
	if err := os.WriteFile(copied, original, 0o600); err != nil {
		t.Fatal(err)
	}
	return live, copied, original
}

func TestInstalledVSCodeKeybindingsCopy(t *testing.T) {
	live, copied, original := installedCopy(t, VSCode)
	if _, _, err := Apply(VSCode, copied, DefaultShortcuts()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Undo(VSCode, copied); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(copied)
	if err != nil || string(restored) != string(original) {
		t.Fatal("live keymap copy did not round-trip")
	}
	stillOriginal, err := os.ReadFile(live)
	if err != nil || string(stillOriginal) != string(original) {
		t.Fatal("real VS Code keybindings changed")
	}
}

func TestInstalledZedKeymapCopyWithConfiguredShortcuts(t *testing.T) {
	live, copied, original := installedCopy(t, Zed)
	shortcuts := DefaultShortcuts()
	shortcuts["Search"] = "ctrl+n"
	if _, _, err := Apply(Zed, copied, shortcuts); err != nil {
		t.Fatal(err)
	}
	current, _ := os.ReadFile(copied)
	if !strings.Contains(string(current), `"ctrl-n"`) || strings.Count(string(current), "terminal::SendKeystroke") < 4 {
		t.Fatal("Zed plan did not include configured tofu shortcuts")
	}
	if _, _, err := undoManaged(copied, "zed"); err != nil {
		t.Fatal(err)
	}
	restored, _ := os.ReadFile(copied)
	if string(restored) != string(original) {
		t.Fatal("Zed keymap copy did not round-trip")
	}
	stillOriginal, _ := os.ReadFile(live)
	if string(stillOriginal) != string(original) {
		t.Fatal("real Zed keymap changed")
	}
}
