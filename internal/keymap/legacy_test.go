package keymap

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeKeymap(t *testing.T, source string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "keymap.json")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestUndoRemovesLegacyBlockExactly(t *testing.T) {
	for _, tc := range []struct{ original, applied string }{
		{
			"// personal keymap\r\n[\r\n  {\"context\": \"Workspace\", \"bindings\": {\"ctrl-a\": \"editor::SelectAll\",},}, // https://example.test/x\r\n]\r\n",
			"// personal keymap\r\n[\r\n  {\"context\": \"Workspace\", \"bindings\": {\"ctrl-a\": \"editor::SelectAll\",},}" + string(legacyBlock(true)) + ", // https://example.test/x\r\n]\r\n",
		},
		{"[]\n", "[" + string(legacyBlock(false)) + "]\n"},
	} {
		path := writeKeymap(t, tc.applied)
		if state, err := Inspect(Zed, path, DefaultShortcuts()); err != nil || state != LegacyOnly {
			t.Fatalf("inspect: %q, %v", state, err)
		}
		status, saved, err := Undo(Zed, path)
		if err != nil || status != "removed tofu Ctrl+K binding" || saved == "" {
			t.Fatalf("undo: %q, %q, %v", status, saved, err)
		}
		backup, err := os.ReadFile(saved)
		if err != nil || string(backup) != tc.applied {
			t.Fatalf("undo backup not exact: %v", err)
		}
		current, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(current, []byte(tc.original)) {
			t.Fatalf("undo did not restore original bytes: %q", current)
		}
	}
}

func TestUndoWhenKeymapDoesNotExist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Zed", "keymap.json")
	if state, err := Inspect(Zed, path, DefaultShortcuts()); err != nil || state != Missing {
		t.Fatalf("inspect: %q, %v", state, err)
	}
	if _, _, err := Undo(Zed, path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("undo created a keymap: %v", err)
	}
}

func TestUndoLeavesUserBindingsAlone(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		want         State
	}{
		{"conflict", `[{"context":"Terminal","bindings":{"ctrl-k":"terminal::Clear"}}]`, Conflicting},
		{"already configured", `[{"context":"Terminal","bindings":{"ctrl-k":["terminal::SendKeystroke","ctrl-k"]}}]`, AlreadyConfigured},
		{"invalid", `[not valid]`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeKeymap(t, tc.source)
			state, err := Inspect(Zed, path, DefaultShortcuts())
			if tc.want == "" && err == nil || tc.want != "" && (err != nil || state != tc.want) {
				t.Fatalf("inspect: %q, %v", state, err)
			}
			if _, _, err := Undo(Zed, path); err != nil {
				t.Fatal(err)
			}
			current, _ := os.ReadFile(path)
			if string(current) != tc.source {
				t.Fatal("keymap was changed")
			}
			entries, _ := os.ReadDir(filepath.Dir(path))
			for _, entry := range entries {
				if strings.Contains(entry.Name(), "backup") {
					t.Fatal("unnecessary backup created")
				}
			}
		})
	}
}
