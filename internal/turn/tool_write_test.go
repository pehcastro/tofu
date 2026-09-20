package turn

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runWrite(t *testing.T, root, path, content string) Result {
	t.Helper()
	tool, err := NewWriteTool(root)
	if err != nil {
		t.Fatalf("building the tool: %v", err)
	}
	args, err := json.Marshal(writeArgs{Path: path, Content: content})
	if err != nil {
		t.Fatal(err)
	}
	result, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	return result
}

func TestWriteSaysItCreatedAFileThatWasNotThere(t *testing.T) {
	result := runWrite(t, t.TempDir(), "note.txt", "one\ntwo\nthree\n")
	t.Logf("result: %q", result.Content)
	if !strings.HasPrefix(result.Content, "created note.txt") {
		t.Fatalf("a new file was not reported as created: %q", result.Content)
	}
	if !strings.Contains(result.Content, "3 lines") {
		t.Fatalf("the created file does not carry its line count: %q", result.Content)
	}
}

func TestWriteReturnsADiffWhenItReplacesAFileThatWasAlreadyThere(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result := runWrite(t, root, "note.txt", "one\ntwo and a half\n")
	t.Logf("result:\n%s", result.Content)
	if strings.Contains(result.Content, "created") {
		t.Fatalf("a replacement was reported as a creation: %q", result.Content)
	}
	for _, want := range []string{"--- note.txt", "-two\n", "+two and a half\n"} {
		if !strings.Contains(result.Content, want) {
			t.Fatalf("the replacement diff does not carry %q:\n%s", want, result.Content)
		}
	}
}

func TestWriteOverAnEmptyFileIsAReplacementAndNotACreation(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "empty.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	result := runWrite(t, root, "empty.txt", "a line\n")
	t.Logf("result:\n%s", result.Content)
	if strings.Contains(result.Content, "created") {
		t.Fatalf("a file that existed and held nothing was reported as created: %q", result.Content)
	}
}

func TestWritingTheTextAFileAlreadyHoldsSaysNothingChanged(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result := runWrite(t, root, "note.txt", "one\n")
	t.Logf("result: %q", result.Content)
	if !strings.Contains(result.Content, "nothing changed") {
		t.Fatalf("an identical write did not say the file is unchanged: %q", result.Content)
	}
}
