package keymap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedRulesAppendAfterUserRulesAndUndoExactly(t *testing.T) {
	for _, original := range []string{
		"[]\n",
		"[\n  {\"key\":\"ctrl+k\",\"command\":\"custom.action\"}\n]\n",
		"[\n  //a user rule\n  {\"key\":\"ctrl+k\",\"command\":\"custom.action\"}, //keep me, too\n]\n",
	} {
		path := filepath.Join(t.TempDir(), "keybindings.json")
		if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
			t.Fatal(err)
		}
		rules := []string{`{"key":"ctrl+k","command":"workbench.action.terminal.sendSequence","args":{"text":"\u000b"},"when":"terminalFocus"}`, `{"key":"alt+k","command":"workbench.action.terminal.sendSequence","args":{"text":"\u001bk"},"when":"terminalFocus"}`}
		if _, _, err := applyManaged(path, "vscode", rules); err != nil {
			t.Fatal(err)
		}
		current, _ := os.ReadFile(path)
		if !strings.Contains(string(current), "tofu-keybindings-v2:begin:vscode") {
			t.Fatal("missing managed block")
		}
		if at := strings.Index(string(current), "custom.action"); at >= 0 && strings.Index(string(current), "sendSequence") < at {
			t.Fatal("tofu rule did not override earlier user rule")
		}
		if state, err := inspectManaged(path, "vscode", rules); err != nil || state != Managed {
			t.Fatalf("inspect: %q %v", state, err)
		}
		if _, _, err := undoManaged(path, "vscode"); err != nil {
			t.Fatal(err)
		}
		restored, _ := os.ReadFile(path)
		if string(restored) != original {
			t.Fatalf("undo changed original bytes: %q", restored)
		}
	}
}

func TestManagedRulesCanBeUpdatedAndRejectEditedBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keymap.json")
	if _, _, err := applyManaged(path, "zed", []string{`{"context":"Terminal","bindings":{"ctrl-k":["terminal::SendKeystroke","ctrl-k"]}}`}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := applyManaged(path, "zed", []string{`{"context":"Terminal","bindings":{"alt-k":["terminal::SendKeystroke","alt-k"]}}`}); err != nil {
		t.Fatal(err)
	}
	if state, err := inspectManaged(path, "zed", []string{`{"context":"Terminal","bindings":{"ctrl-n":["terminal::SendKeystroke","ctrl-n"]}}`}); err != nil || state != NeedsUpdate {
		t.Fatalf("stale plan: %q %v", state, err)
	}
	current, _ := os.ReadFile(path)
	if strings.Contains(string(current), "ctrl-k") || !strings.Contains(string(current), "alt-k") {
		t.Fatal("new plan did not replace old managed plan")
	}
	modified := strings.Replace(string(current), "alt-k", "alt-v", 1)
	if err := os.WriteFile(path, []byte(modified), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := undoManaged(path, "zed"); err == nil {
		t.Fatal("removed edited managed block")
	}
}
