package turn

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

func appendThrough(t *testing.T, dir string, ledger *ReadLedger, content string) (Result, error) {
	t.Helper()
	tool, err := NewWriteTool(dir)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]any{"path": "a.test.ts", "content": content, "append": true})
	if err != nil {
		t.Fatal(err)
	}
	return tool.Reading(ledger).Run(context.Background(), raw)
}

func TestWriteAppendAfterRead(t *testing.T) {
	for _, row := range []struct {
		name, file, content, want, shown string
	}{
		{"final newline", "one\n", "\ntwo\n", "one\n\ntwo\n", "2\t\n3\ttwo"},
		{"no final newline", "one", "two\n", "one\ntwo\n", "2\ttwo"},
		{"crlf file", "one\r\ntwo", "three\nfour\n", "one\r\ntwo\r\nthree\r\nfour\r\n", "3\tthree\n4\tfour"},
		{"crlf content into lf", "one\n", "two\r\n", "one\ntwo\n", "2\ttwo"},
		{"mixed endings stay lf", "one\r\ntwo\n", "three", "one\r\ntwo\nthree", "3\tthree"},
	} {
		t.Run(row.name, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "a.test.ts")
			if err := os.WriteFile(target, []byte(row.file), 0o644); err != nil {
				t.Fatal(err)
			}
			ledger := NewReadLedger()
			ledger.Mark("a.test.ts", []byte(row.file))
			result, err := appendThrough(t, dir, ledger, row.content)
			if err != nil {
				t.Fatal(err)
			}
			if got, _ := os.ReadFile(target); string(got) != row.want {
				t.Fatalf("a.test.ts holds %q, want %q", got, row.want)
			}
			if !strings.Contains(result.Content, row.shown) {
				t.Fatalf("result %q does not show %q", result.Content, row.shown)
			}
			if _, err := appendThrough(t, dir, ledger, "again\n"); err != nil {
				t.Fatalf("a second append after the first, with no read between: %v", err)
			}
		})
	}
}

func TestWriteAppendRefused(t *testing.T) {
	for _, row := range []struct {
		name, read, content, says string
		missing                   bool
	}{
		{name: "never read", content: "two\n", says: "read"},
		{name: "changed since read", read: "older\n", content: "two\n", says: "read"},
		{name: "empty content", read: "one\n", content: "", says: "nothing"},
		{name: "missing file", read: "one\n", content: "two\n", says: "without append", missing: true},
	} {
		t.Run(row.name, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "a.test.ts")
			if !row.missing {
				if err := os.WriteFile(target, []byte("one\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			ledger := NewReadLedger()
			if row.read != "" {
				ledger.Mark("a.test.ts", []byte(row.read))
			}
			_, err := appendThrough(t, dir, ledger, row.content)
			if err == nil || !strings.Contains(err.Error(), row.says) {
				t.Fatalf("append error %v, want one saying %q", err, row.says)
			}
			if got, _ := os.ReadFile(target); !row.missing && string(got) != "one\n" {
				t.Fatalf("a.test.ts holds %q after a refusal, want %q", got, "one\n")
			}
			if _, statErr := os.Stat(target); row.missing && statErr == nil {
				t.Fatal("a refused append to a missing file created it")
			}
		})
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

func numbered(prefix string, count int) string {
	var lines strings.Builder
	for i := 1; i <= count; i++ {
		fmt.Fprintf(&lines, "%s%d()\n", prefix, i)
	}
	return lines.String()
}

func TestWriteOmissionPlaceholder(t *testing.T) {
	sixty, kept := numbered("call", 60), numbered("call", 10)
	for _, row := range []struct {
		name, old, content, says string
		missing                  bool
	}{
		{name: "ellipsis first", old: sixty, content: kept + "// ... rest unchanged\n", says: "line 11"},
		{name: "python existing code", old: sixty, content: kept + "    # ... existing code ...\n", says: "line 11"},
		{name: "unicode ellipsis", old: sixty, content: "// rest of the file …\n" + kept, says: "line 1:"},
		{name: "crlf", old: sixty, content: strings.ReplaceAll(kept, "\n", "\r\n") + "/* ... unchanged ... */\r\n", says: "line 11"},
		{name: "two placeholders", old: sixty, content: "// ... previous code ...\n" + kept + "// ... remaining calls\n", says: `line 1: "// ... previous code ...", line 12: "// ... remaining calls"`},
		{name: "already in the old file", old: "// ... rest unchanged\n" + sixty, content: "// ... rest unchanged\n" + kept},
		{name: "new file", missing: true, content: kept + "// ... rest unchanged\n"},
		{name: "loses only a few lines", old: numbered("call", 3), content: "call1()\n// ... other methods\n"},
		{name: "spread syntax", old: sixty, content: kept + "return { ...previous, notice }\n"},
		{name: "placeholder after code", old: sixty, content: kept + "x() " + "// ... rest unchanged\n"},
	} {
		t.Run(row.name, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "a.go")
			if !row.missing {
				if err := os.WriteFile(target, []byte(row.old), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			err := writeThrough(t, dir, "a.go", row.content)
			got, _ := os.ReadFile(target)
			if row.says == "" {
				if err != nil || string(got) != row.content {
					t.Fatalf("write refused with %v, file holds %q, want it written", err, got)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), row.says) || !strings.Contains(err.Error(), "edit") {
				t.Fatalf("write error %v, want one naming %q and pointing at edit", err, row.says)
			}
			if string(got) != row.old {
				t.Fatalf("a.go holds %d bytes after the refusal, want the old %d unchanged", len(got), len(row.old))
			}
		})
	}
}
