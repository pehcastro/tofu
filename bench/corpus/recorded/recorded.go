package recorded

import (
	"errors"
	"io/fs"
	"os"
	"testing"

	"tofu/internal/sys"
)

func State(t testing.TB, name string) string {
	t.Helper()
	return Need(t, sys.RecordedStateDir(name), "drive tofu in this repository under this home to record it")
}

func Need(t testing.TB, path, produce string) string {
	t.Helper()
	if path == "" {
		t.Skipf("needs a recorded corpus and this home has no owner state to hold one: %s", produce)
	}
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		t.Skipf("needs %s, absent on this machine: %s", path, produce)
	}
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	empty := info.Size() == 0
	if info.IsDir() {
		entries, err := os.ReadDir(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		empty = len(entries) == 0
	}
	if empty {
		t.Fatalf("%s is present and empty", path)
	}
	return path
}
