package turn

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeThrough(t *testing.T, dir, path, content string) error {
	t.Helper()
	tool, err := NewWriteTool(dir)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(writeArgs{Path: path, Content: content})
	if err != nil {
		t.Fatal(err)
	}
	_, err = tool.Run(context.Background(), raw)
	return err
}

func TestWriteInPlaceWhileAnotherHandleIsOpen(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(target, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	held, err := os.Open(target)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Close() }()
	if err := writeThrough(t, dir, "x.txt", "new\n"); err != nil {
		t.Fatalf("write over a file another handle holds open: %v", err)
	}
	if got, _ := os.ReadFile(target); string(got) != "new\n" {
		t.Fatalf("x.txt holds %q, want %q", got, "new\n")
	}
}

func TestWriteShorterContentLeavesNoOldTail(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.txt"), []byte("a much longer old body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeThrough(t, dir, "x.txt", "short\n"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "x.txt")); string(got) != "short\n" {
		t.Fatalf("x.txt holds %q, want %q", got, "short\n")
	}
}

func TestWriteKeepsModeAndFollowsSymlink(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "run.sh")
	if err := os.WriteFile(script, []byte("old\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(script)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeThrough(t, dir, "run.sh", "new\n"); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(script)
	if err != nil {
		t.Fatal(err)
	}
	if after.Mode() != before.Mode() {
		t.Fatalf("run.sh mode %v after the write, want %v", after.Mode(), before.Mode())
	}
	if err := os.WriteFile(filepath.Join(dir, "real.txt"), []byte("target old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real.txt", filepath.Join(dir, "link.txt")); err != nil {
		t.Skipf("this host cannot make a symlink: %v", err)
	}
	if err := writeThrough(t, dir, "link.txt", "target new\n"); err != nil {
		t.Fatal(err)
	}
	link, err := os.Lstat(filepath.Join(dir, "link.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if link.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link.txt is %v after the write, want a symlink", link.Mode())
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "real.txt")); string(got) != "target new\n" {
		t.Fatalf("real.txt holds %q, want %q", got, "target new\n")
	}
}

func TestWriteCreatesNewFileInNewDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := writeThrough(t, dir, "a/b/new.txt", "fresh\n"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "a", "b", "new.txt")); string(got) != "fresh\n" {
		t.Fatalf("new.txt holds %q, want %q", got, "fresh\n")
	}
}
