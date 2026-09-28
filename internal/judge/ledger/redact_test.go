package ledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/sys"
)

func TestAKeyInAJudgedCommandNeverReachesTheLedgerLineOrTheElidedState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	t.Setenv(sys.OpenRouterKeyName, "")
	const key = "sk-or-v1-made-up-for-the-ledger-8Tn3"
	if err := sys.SaveKey(sys.OpenRouterKeyName, key); err != nil {
		t.Fatalf("storing the key: %v", err)
	}
	dir := t.TempDir()
	writer := NewWriter(dir)
	for _, padding := range []int{0, stateInlineCeiling} {
		state, err := json.Marshal(map[string]string{
			"command": "curl -H 'Authorization: Bearer " + key + "' https://openrouter.ai",
			"env":     "OPENROUTER_KEY=\"" + key + "\"\nNEXT=1",
			"padding": strings.Repeat("x", padding),
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Append(Row{Point: "tool_gate", State: state}); err != nil {
			t.Fatalf("appending a row with %d bytes of padding: %v", padding, err)
		}
	}
	var written string
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		body, err := os.ReadFile(path)
		written += string(body)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(written, key) {
		t.Fatalf("the ledger holds the key of length %d", len(key))
	}
	if strings.Count(written, sys.KeyRedactedMark) < 4 {
		t.Fatalf("the ledger shows %d redaction marks, want both fields of both rows", strings.Count(written, sys.KeyRedactedMark))
	}
}
