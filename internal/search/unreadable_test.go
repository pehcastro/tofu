package search

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestUnreadableFileIsCountedAndSearchGoesOn(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("needle here\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "folder"), 0o700); err != nil {
		t.Fatal(err)
	}
	files := []string{"gone.txt", "a.txt", "folder"}
	for _, row := range []struct {
		pattern string
		units   int
		absence string
	}{
		{pattern: "needle", units: 1},
		{pattern: "NEEDLE", units: 1},
		{pattern: "absent", units: 0, absence: "every readable file was read, and the unreadable ones may hold a match"},
	} {
		result, err := Find(Request{Root: root, Files: files, Pattern: regexp.MustCompile(row.pattern)})
		if err != nil {
			t.Fatalf("%s: one unreadable file failed the whole search: %v", row.pattern, err)
		}
		if len(result.Units) != row.units {
			t.Fatalf("%s: %d units, want %d:\n%s", row.pattern, len(result.Units), row.units, result.Text)
		}
		if !strings.Contains(result.Text, "2 files could not be read, the first gone.txt: ") {
			t.Fatalf("%s: no line counting each unreadable file once and naming the first in list order by its project path:\n%s", row.pattern, result.Text)
		}
		if strings.Contains(result.Text, root) {
			t.Fatalf("%s: the reason carries the absolute path:\n%s", row.pattern, result.Text)
		}
		if strings.Contains(result.Text, "the files were read") || (row.absence != "" && !strings.Contains(result.Text, row.absence)) {
			t.Fatalf("%s: the absence claims every file was read:\n%s", row.pattern, result.Text)
		}
	}
}
