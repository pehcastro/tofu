package turn

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadToolReadsAFileInsideTheRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("seeding the fixture: %v", err)
	}
	tool, err := NewReadTool(root)
	if err != nil {
		t.Fatalf("building the tool: %v", err)
	}
	result, err := tool.Run(context.Background(), json.RawMessage(`{"path":"a.txt"}`))
	if err != nil {
		t.Fatalf("running the tool: %v", err)
	}
	if result.Content != "hello" {
		t.Fatalf("content was %q", result.Content)
	}
}

func TestReadToolRefusesAPathOutsideTheRoot(t *testing.T) {
	root := t.TempDir()
	tool, err := NewReadTool(root)
	if err != nil {
		t.Fatalf("building the tool: %v", err)
	}
	_, err = tool.Run(context.Background(), json.RawMessage(`{"path":"../outside.txt"}`))
	if err == nil {
		t.Fatal("expected the tool to refuse a path that escapes the root")
	}
	if !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("error did not name the reason: %v", err)
	}
}

func TestReadToolRefusesAnAbsolutePath(t *testing.T) {
	root := t.TempDir()
	tool, err := NewReadTool(root)
	if err != nil {
		t.Fatalf("building the tool: %v", err)
	}
	abs := filepath.Join(root, "a.txt")
	_, err = tool.Run(context.Background(), json.RawMessage(`{"path":"`+filepath.ToSlash(abs)+`"}`))
	if err == nil {
		t.Fatal("expected the tool to refuse an absolute path")
	}
}

func TestWriteToolWritesInsideTheRootAndRefusesOutsideIt(t *testing.T) {
	root := t.TempDir()
	tool, err := NewWriteTool(root)
	if err != nil {
		t.Fatalf("building the tool: %v", err)
	}
	if _, err := tool.Run(context.Background(), json.RawMessage(`{"path":"b.txt","content":"hi"}`)); err != nil {
		t.Fatalf("writing inside the root: %v", err)
	}
	written, err := os.ReadFile(filepath.Join(root, "b.txt"))
	if err != nil {
		t.Fatalf("reading back the write: %v", err)
	}
	if string(written) != "hi" {
		t.Fatalf("wrote %q", written)
	}

	_, err = tool.Run(context.Background(), json.RawMessage(`{"path":"../escape.txt","content":"hi"}`))
	if err == nil {
		t.Fatal("expected the tool to refuse a path that escapes the root")
	}
	if _, statErr := os.Stat(filepath.Join(root, "..", "escape.txt")); statErr == nil {
		t.Fatal("the write escaped the root")
	}
}

func TestBashToolRunsWithItsWorkingDirectoryPinnedToTheRoot(t *testing.T) {
	root := t.TempDir()
	tool, err := NewBashTool(root)
	if err != nil {
		t.Fatalf("building the tool: %v", err)
	}
	result, err := tool.Run(context.Background(), json.RawMessage(`{"command":"pwd"}`))
	if err != nil {
		t.Fatalf("running the tool: %v", err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("resolving the fixture root: %v", err)
	}
	reported := filepath.ToSlash(strings.TrimSpace(result.Content))
	wantTail := filepath.ToSlash(filepath.Join(filepath.Base(filepath.Dir(resolvedRoot)), filepath.Base(resolvedRoot)))
	if !strings.HasSuffix(reported, wantTail) {
		t.Fatalf("pwd reported %q, wanted a path ending %q: the shell is msys and reports a posix path for the same directory", reported, wantTail)
	}
}

func TestBashToolRecordsTheExitCode(t *testing.T) {
	root := t.TempDir()
	tool, err := NewBashTool(root)
	if err != nil {
		t.Fatalf("building the tool: %v", err)
	}
	result, err := tool.Run(context.Background(), json.RawMessage(`{"command":"exit 7"}`))
	if err != nil {
		t.Fatalf("running the tool: %v", err)
	}
	if result.ExitCode == nil || *result.ExitCode != 7 {
		t.Fatalf("expected exit code 7, got %+v", result.ExitCode)
	}
}

func TestBashToolRefusesAnEmptyCommand(t *testing.T) {
	root := t.TempDir()
	tool, err := NewBashTool(root)
	if err != nil {
		t.Fatalf("building the tool: %v", err)
	}
	_, err = tool.Run(context.Background(), json.RawMessage(`{"command":""}`))
	if err == nil {
		t.Fatal("expected the tool to refuse an empty command")
	}
}

func TestConfineRejectsASymlinkOrDeepEscape(t *testing.T) {
	root := t.TempDir()
	if _, err := confine(root, "sub/../../outside"); err == nil {
		t.Fatal("expected a nested escape to be rejected")
	}
	if _, err := confine(root, "sub/inside"); err != nil {
		t.Fatalf("did not expect a nested but confined path to be rejected: %v", err)
	}
}
