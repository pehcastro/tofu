package snapshot

import (
	"context"
	"errors"
	"io/fs"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const randomBytes = 2 << 20

func randomText(seed uint64) string {
	source := rand.NewChaCha8([32]byte{byte(seed)})
	body := make([]byte, randomBytes)
	_, _ = source.Read(body)
	return string(body)
}

func (p project) session(root string) project {
	p.repo.Session = filepath.Join(p.repo.State, "sessions", root)
	return p
}

func (p project) storeBytes() int64 {
	p.t.Helper()
	var total int64
	err := filepath.WalkDir(filepath.Join(p.repo.State, gitDirName), func(_ string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		info, err := entry.Info()
		total += info.Size()
		return err
	})
	if err != nil {
		p.t.Fatal(err)
	}
	return total
}

func (p project) has(object string) bool {
	return exec.Command("git", "--git-dir", filepath.Join(p.repo.State, gitDirName), "cat-file", "-e", object).Run() == nil
}

func (p project) blob(body string) string {
	p.t.Helper()
	cmd := exec.Command("git", "hash-object", "--stdin")
	cmd.Stdin = strings.NewReader(body)
	out, err := cmd.Output()
	if err != nil {
		p.t.Fatal(err)
	}
	return string(out[:len(out)-1])
}

func (p project) age(path string, by time.Duration) {
	p.t.Helper()
	at := time.Now().Add(-by)
	err := filepath.WalkDir(path, func(file string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Chtimes(file, at, at)
	})
	if err != nil {
		p.t.Fatal(err)
	}
}

func TestTwoSessionsOfOneProjectShareOneObjectStore(t *testing.T) {
	first := newProject(t)
	first.write("big.bin", randomText(1))
	first.turn("t1", func() { first.write("kept.txt", "first session\n") })
	once := first.storeBytes()

	second := first.session("other")
	second.turn("t2", func() { second.write("kept.txt", "second session\n") })
	if twice := second.storeBytes(); twice >= 2*once || twice-once > randomBytes/2 {
		t.Errorf("the store went from %d to %d bytes for a second session over an unchanged 2 MiB file", once, twice)
	}
	if _, err := os.Stat(filepath.Join(first.repo.Session, gitDirName)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a session folder holds its own git dir: %v", err)
	}
	second.undo(Ask{Turns: 1})
	second.want("kept.txt", "first session\n")
}

func TestPruneDropsAnOldSessionAndTheObjectsOnlyItHeld(t *testing.T) {
	old := newProject(t)
	gone := randomText(2)
	old.turn("t1", func() { old.write("only-old.bin", gone) })
	old.turn("t2", func() { old.write("kept.txt", "old session\n") })
	if err := os.Remove(old.path("only-old.bin")); err != nil {
		t.Fatal(err)
	}
	old.age(old.repo.Session, 8*24*time.Hour)

	live := old.session("live")
	live.turn("t3", func() { live.write("kept.txt", "live session\n") })
	old.age(filepath.Join(old.repo.State, gitDirName, "objects"), 2*time.Hour)
	if err := live.repo.Prune(context.Background()); err != nil {
		t.Fatalf("prune: %v", err)
	}
	if _, err := os.Stat(filepath.Join(old.repo.Session, ledgerName)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the ledger of a session older than the lifetime is still there: %v", err)
	}
	if old.has(old.blob(gone)) {
		t.Error("a blob only the dropped session referred to survived the prune")
	}
	if !old.has(old.blob("old session\n")) {
		t.Error("a blob the live session's start tree holds was pruned")
	}
	live.undo(Ask{Turns: 1})
	live.want("kept.txt", "old session\n")
}

func TestPruneKeepsObjectsYoungerThanTheGrace(t *testing.T) {
	p := newProject(t)
	p.turn("t1", func() { p.write("kept.txt", "written\n") })
	p.age(p.repo.Session, 8*24*time.Hour)
	if err := p.session("fresh").repo.Prune(context.Background()); err != nil {
		t.Fatalf("prune: %v", err)
	}
	if !p.has(p.blob("written\n")) {
		t.Error("an object written minutes ago was pruned, so a turn still writing its tree would lose it")
	}
}

func TestUndoNamesATurnWhoseTreeIsGone(t *testing.T) {
	p := newProject(t)
	p.turn("t1", func() { p.write("kept.txt", "written\n") })
	entries, err := p.repo.entries()
	if err != nil {
		t.Fatal(err)
	}
	start := entries[0].Start
	if err := os.Remove(filepath.Join(p.repo.State, gitDirName, "objects", start[:2], start[2:])); err != nil {
		t.Fatal(err)
	}
	var pruned Pruned
	if _, err := p.repo.Undo(context.Background(), Ask{Turns: 1}); !errors.As(err, &pruned) || pruned.Turn != "t1" {
		t.Errorf("undo over a missing start tree said %v", err)
	}
}
