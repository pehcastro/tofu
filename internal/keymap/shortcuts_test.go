package keymap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShortcutsPersistAndRejectReservedKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keybindings.json")
	shortcuts := DefaultShortcuts()
	shortcuts["Search"] = "ctrl+p"
	if err := SaveShortcuts(path, shortcuts); err != nil {
		t.Fatal(err)
	}
	if got := LoadShortcuts(path)["Search"]; got != "ctrl+p" {
		t.Fatalf("persisted shortcut: %q", got)
	}
	if err := ValidShortcut("ctrl+c"); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("reserved quit key was not rejected: %v", err)
	}
	shortcuts["Search"] = ""
	if err := SaveShortcuts(path, shortcuts); err != nil {
		t.Fatal(err)
	}
	if LoadShortcuts(path)["Search"] != "" {
		t.Fatal("clearing shortcut did not persist")
	}
}

func TestShortcutFileRejectsDuplicateBindings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keybindings.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"bindings":{"Search":"alt+k"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	bindings := LoadShortcuts(path)
	if bindings["Search"] != "ctrl+k" || bindings["Commands"] != "alt+k" {
		t.Fatal("duplicate saved binding was accepted")
	}
}
