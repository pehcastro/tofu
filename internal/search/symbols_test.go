package search

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestFindsSymbolPassParsesTheBodiesTheScanHeldWithoutReadingAgain(t *testing.T) {
	matched := map[string]string{
		"a.go":   "package a\n\nfunc Target() {}\n",
		"b.go":   "package a\n\nfunc use() { Target() }\n",
		"bad.go": "package a\n\nfunc broken( {\n",
		"c.txt":  "Target\n",
	}
	attempt := goSymbolAttempt("Target", matched)
	if attempt.Lines != 2 || attempt.Files != 2 || attempt.NotRun != "" {
		t.Fatalf("%d lines in %d files, not run %q, want 2 lines in 2 files from the held bodies", attempt.Lines, attempt.Files, attempt.NotRun)
	}
}

func TestSymbolPassSkipsAFileGoneUnreadableAfterTheScan(t *testing.T) {
	root := t.TempDir()
	for rel, body := range map[string]string{
		"a.go":      "package a\n\nfunc Target() {}\n",
		"b.go":      "package a\n\nfunc use() { Target() }\n",
		"gone.go":   "package a\n\nfunc other() { Target() }\n",
		"folder.go": "package a\n\nfunc more() { Target() }\n",
	} {
		if err := os.WriteFile(filepath.Join(root, rel), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	files := []string{"a.go", "gone.go", "b.go", "folder.go"}
	result, err := Find(Request{Root: root, Files: files, Pattern: regexp.MustCompile("Target")})
	if err != nil || len(result.Units) != 4 {
		t.Fatalf("the scan does not match all four files before they go unreadable: %v\n%s", err, result.Text)
	}
	if err := os.Remove(filepath.Join(root, "gone.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "folder.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "folder.go"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		name        string
		definitions int
		callers     int
		absence     string
	}{
		{name: "Target", definitions: 1, callers: 1},
		{name: "Absent", absence: "every readable go file was parsed, and the unreadable ones may declare or call Absent"},
	} {
		graph, err := Symbols(root, files, row.name)
		if err != nil {
			t.Fatalf("%s: one unreadable file failed the whole symbol pass: %v", row.name, err)
		}
		if len(graph.Definitions) != row.definitions || len(graph.Callers) != row.callers || graph.Parsed != 2 || graph.Unparsed != 0 {
			t.Fatalf("%s: %d definitions, %d callers, %d parsed, %d unparsed, want %d, %d, 2, 0:\n%s",
				row.name, len(graph.Definitions), len(graph.Callers), graph.Parsed, graph.Unparsed, row.definitions, row.callers, graph.Text)
		}
		if !strings.Contains(graph.Text, "2 files could not be read, the first gone.go: ") {
			t.Fatalf("%s: no line counting each unreadable file once and naming the first in list order by its project path:\n%s", row.name, graph.Text)
		}
		if strings.Contains(graph.Text, root) {
			t.Fatalf("%s: the reason carries the absolute path:\n%s", row.name, graph.Text)
		}
		if strings.Contains(graph.Text, "the files were parsed") || (row.absence != "" && !strings.Contains(graph.Text, row.absence)) {
			t.Fatalf("%s: the absence claims every file was parsed:\n%s", row.name, graph.Text)
		}
	}
}
