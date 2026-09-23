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

func TestWriteReplacingAFileThisTurnHasNotReadIsRefusedWithItsContent(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("kept as is\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tool, err := NewWriteTool(root)
	if err != nil {
		t.Fatalf("building the tool: %v", err)
	}
	tool = tool.Reading(NewReadLedger())

	args, err := json.Marshal(writeArgs{Path: "note.txt", Content: "overwritten\n"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = tool.Run(context.Background(), args)
	if err == nil {
		t.Fatal("a write over a file this turn has not read must be refused")
	}
	for _, fact := range []string{"note.txt exists and has not been read", "kept as is"} {
		if !strings.Contains(err.Error(), fact) {
			t.Fatalf("the refusal does not name %q: %v", fact, err)
		}
	}
	if got, readErr := os.ReadFile(filepath.Join(root, "note.txt")); readErr != nil || strings.Contains(string(got), "overwritten") {
		t.Fatalf("a refused write changed the file: %q, %v", got, readErr)
	}
}

func TestWriteThatCreatesAFileIsNotRefusedByTheReadGate(t *testing.T) {
	root := t.TempDir()
	tool, err := NewWriteTool(root)
	if err != nil {
		t.Fatalf("building the tool: %v", err)
	}
	tool = tool.Reading(NewReadLedger())

	args, err := json.Marshal(writeArgs{Path: "new.txt", Content: "born here\n"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Run(context.Background(), args); err != nil {
		t.Fatalf("creating a file needs no prior read: %v", err)
	}
}
