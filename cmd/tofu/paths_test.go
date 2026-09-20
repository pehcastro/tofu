package main

import (
	"bytes"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/sys"
)

func legacyTree(t *testing.T) string {
	t.Helper()
	parent := t.TempDir()
	for path, body := range map[string]string{
		"sessions/turn-a/header.json":  `{"id":"turn-a"}`,
		"sessions/turn-a/events.jsonl": "{\"kind\":\"step\"}\n",
		"sessions/turn-b/header.json":  `{"id":"turn-b"}`,
		"log/decisions.jsonl":          "{\"point\":\"tool_gate\"}\n",
		"calibration/tool_gate.lock":   "threshold: 0.8\n",
	} {
		full := filepath.Join(parent, sys.LegacyStateDirName, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return parent
}

func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = string(body)
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	return files
}

func sameTree(t *testing.T, want, got map[string]string, what string) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("%s: %d files, want %d\n%v\n%v", what, len(got), len(want), got, want)
	}
	for name, body := range want {
		if got[name] != body {
			t.Fatalf("%s: %s reads %q, want %q", what, name, got[name], body)
		}
	}
}

func TestTheFirstRunCopiesTheOldDirectoryAndLeavesItByteForByte(t *testing.T) {
	parent := legacyTree(t)
	old := filepath.Join(parent, sys.LegacyStateDirName)
	before := snapshot(t, old)

	var out bytes.Buffer
	copyLegacyStateDir(&out, parent)

	sameTree(t, before, snapshot(t, old), "the old directory changed")
	sameTree(t, before, snapshot(t, filepath.Join(parent, sys.StateDirName)), "the copy does not match the old directory")

	said := out.String()
	for _, want := range []string{".tofu", ".boji", "2 sessions", "5 files", "still there"} {
		if !strings.Contains(said, want) {
			t.Errorf("the sentence does not say %q:\n%s", want, said)
		}
	}
	t.Log(said)
}

func TestTheSecondRunCopiesNothingAndSaysNothing(t *testing.T) {
	parent := legacyTree(t)
	var first bytes.Buffer
	copyLegacyStateDir(&first, parent)
	if first.Len() == 0 {
		t.Fatal("the first run said nothing, so there is no second run to test")
	}
	copied := snapshot(t, filepath.Join(parent, sys.StateDirName))

	var second bytes.Buffer
	copyLegacyStateDir(&second, parent)

	if second.Len() != 0 {
		t.Fatalf("the second run printed:\n%s", second.String())
	}
	sameTree(t, copied, snapshot(t, filepath.Join(parent, sys.StateDirName)), "the second run wrote into the new directory")
}

type refusingFS struct {
	fs.FS
	refuse string
}

func (r refusingFS) Open(name string) (fs.File, error) {
	if name == r.refuse {
		return nil, fs.ErrPermission
	}
	return r.FS.Open(name)
}

func TestACopyInterruptedPartWayNeverBecomesTheNewDirectory(t *testing.T) {
	parent := legacyTree(t)
	source := filepath.Join(parent, sys.LegacyStateDirName)
	target := filepath.Join(parent, sys.StateDirName)
	partial := target + partialSuffix

	if _, err := copyTreeInto(refusingFS{FS: os.DirFS(source), refuse: "log/decisions.jsonl"}, partial); err == nil {
		t.Fatal("the copy reported success over a source it could not read whole")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("an interrupted copy created %s", target)
	}
	if half := snapshot(t, partial); len(half) != 1 {
		t.Fatalf("the interrupted copy holds %d files, want the one it managed: %v", len(half), half)
	}
	if dir := sys.StateDir(parent); dir != source {
		t.Fatalf("after an interrupted copy the resolver reads %s, want the old directory", dir)
	}

	var out bytes.Buffer
	copyLegacyStateDir(&out, parent)

	if _, err := os.Stat(partial); !os.IsNotExist(err) {
		t.Fatalf("the interrupted copy survived at %s", partial)
	}
	sameTree(t, snapshot(t, source), snapshot(t, target), "the run after an interruption did not copy the whole tree")
}

func TestSomethingAlreadyAtTheNewPathIsLeftAlone(t *testing.T) {
	parent := legacyTree(t)
	target := filepath.Join(parent, sys.StateDirName)
	his := "a file of his, not a data directory"
	if err := os.WriteFile(target, []byte(his), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	copyLegacyStateDir(&out, parent)

	if out.Len() != 0 {
		t.Fatalf("the run printed:\n%s", out.String())
	}
	body, err := os.ReadFile(target)
	if err != nil || string(body) != his {
		t.Fatalf("%s was overwritten: %q, %v", target, string(body), err)
	}
}

func TestAHalfCopyLeftBehindIsThrownAwayAndTheNextRunStartsOver(t *testing.T) {
	parent := legacyTree(t)
	partial := filepath.Join(parent, sys.StateDirName+partialSuffix)
	if err := os.MkdirAll(filepath.Join(partial, "sessions", "turn-a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(partial, "sessions", "turn-a", "header.json"), []byte("half"), 0o644); err != nil {
		t.Fatal(err)
	}
	if dir := sys.StateDir(parent); dir != filepath.Join(parent, sys.LegacyStateDirName) {
		t.Fatalf("a half copy was read as the data directory: %s", dir)
	}

	var out bytes.Buffer
	copyLegacyStateDir(&out, parent)

	if _, err := os.Stat(partial); !os.IsNotExist(err) {
		t.Fatalf("the half copy survived at %s", partial)
	}
	sameTree(t, snapshot(t, filepath.Join(parent, sys.LegacyStateDirName)),
		snapshot(t, filepath.Join(parent, sys.StateDirName)), "the copy that started over is not complete")
}

func TestWithTheNewDirectoryPresentTheOldOneIsNeverRead(t *testing.T) {
	parent := legacyTree(t)
	copyLegacyStateDir(io.Discard, parent)

	old := filepath.Join(parent, sys.LegacyStateDirName)
	if err := os.RemoveAll(old); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte("reading this as a directory fails"), 0o000); err != nil {
		t.Fatal(err)
	}

	var second bytes.Buffer
	copyLegacyStateDir(&second, parent)
	if second.Len() != 0 {
		t.Fatalf("the old directory was touched:\n%s", second.String())
	}
	if dir := sys.StateDir(parent); dir != filepath.Join(parent, sys.StateDirName) {
		t.Fatalf("the resolver reads %s with the new directory present", dir)
	}
	if _, err := os.ReadDir(old); err == nil {
		t.Fatal("the old path is still readable as a directory, so this test proves nothing")
	}
}
