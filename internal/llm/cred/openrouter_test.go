package cred

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const storedKey = "or-key-not-a-real-credential"

func TestSaveOpenRouterKeepsTheOtherVariablesAndReplacesTheKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	existing := "# notes\nOTHER=1\nexport OPENROUTER_KEY=an-older-key\n"
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := SaveOpenRouter(path, storedKey); err != nil {
		t.Fatalf("save: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	got := string(raw)
	want := "# notes\nOTHER=1\nOPENROUTER_KEY=" + storedKey + "\n"
	if got != want {
		t.Fatalf("stored file is %q, want %q", got, want)
	}
	if strings.Count(got, "OPENROUTER_KEY=") != 1 {
		t.Fatalf("stored file carries the key twice: %q", got)
	}
}

func TestSaveOpenRouterWritesAFileOnlyTheOwnerCanRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "made", "up", ".env")
	if err := SaveOpenRouter(path, "  "+storedKey+"\n"); err != nil {
		t.Fatalf("save: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != openRouterFileMode {
		t.Fatalf("mode is %v, want %v", info.Mode().Perm(), os.FileMode(openRouterFileMode))
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(raw) != "OPENROUTER_KEY="+storedKey+"\n" {
		t.Fatalf("stored file is %q", string(raw))
	}
}

func TestSaveOpenRouterRefusesAKeyItCannotStore(t *testing.T) {
	dir := t.TempDir()
	for name, key := range map[string]string{"empty": "   ", "two lines": "one\ntwo"} {
		path := filepath.Join(dir, name+".env")
		if err := SaveOpenRouter(path, key); err == nil {
			t.Errorf("a %s key was accepted", name)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("a %s key wrote %s", name, path)
		}
	}
}
