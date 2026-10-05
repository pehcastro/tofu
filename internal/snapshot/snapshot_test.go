package snapshot

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

const (
	crlfText   = "one\r\ntwo\r\n"
	binaryText = "\x00\x01\r\n\xff\xfe\r\x00"
	largeBytes = 11 << 20
)

type project struct {
	t    *testing.T
	repo Repo
}

func newProject(t *testing.T) project {
	t.Helper()
	state := t.TempDir()
	p := project{t: t, repo: Repo{State: state, Session: filepath.Join(state, "sessions", "root"), Tree: t.TempDir()}}
	p.write(".gitattributes", "* text=auto eol=lf\n")
	p.write(".gitignore", "ignored.log\n")
	p.write("crlf.txt", crlfText)
	p.write("binary.dat", binaryText)
	p.write("gone.txt", "deleted by the turn\n")
	p.write("kept.txt", "edited by the turn\n")
	p.write("ignored.log", "before\n")
	return p
}

func (p project) path(name string) string {
	return filepath.Join(p.repo.Tree, filepath.FromSlash(name))
}

func (p project) write(name, body string) {
	p.t.Helper()
	if err := os.MkdirAll(filepath.Dir(p.path(name)), 0o755); err != nil {
		p.t.Fatal(err)
	}
	if err := os.WriteFile(p.path(name), []byte(body), 0o644); err != nil {
		p.t.Fatal(err)
	}
}

func (p project) read(name string) string {
	p.t.Helper()
	body, err := os.ReadFile(p.path(name))
	if errors.Is(err, os.ErrNotExist) {
		return "<absent>"
	}
	if err != nil {
		p.t.Fatal(err)
	}
	return string(body)
}

func (p project) turn(name string, change func()) {
	p.t.Helper()
	if err := p.repo.Begin(context.Background(), name); err != nil {
		p.t.Fatalf("begin %s: %v", name, err)
	}
	change()
	if err := p.repo.End(context.Background()); err != nil {
		p.t.Fatalf("end %s: %v", name, err)
	}
}

func (p project) undo(ask Ask) Report {
	p.t.Helper()
	report, err := p.repo.Undo(context.Background(), ask)
	if err != nil {
		p.t.Fatalf("undo %+v: %v", ask, err)
	}
	return report
}

func (p project) want(name, body string) {
	p.t.Helper()
	if got := p.read(name); got != body {
		p.t.Errorf("%s holds %q, want %q", name, got, body)
	}
}

func (p project) everyKindOfChange() {
	p.write("crlf.txt", "changed\r\n")
	p.write("binary.dat", "\x00changed")
	p.write("kept.txt", "the turn wrote this\n")
	p.write("made/deep/new.txt", "created by the turn\n")
	p.write("ignored.log", "after\n")
	p.write("large.bin", strings.Repeat("x", largeBytes))
	if err := os.Remove(p.path("gone.txt")); err != nil {
		p.t.Fatal(err)
	}
}

func TestUndoPutsBackEveryKindOfChangeByteForByte(t *testing.T) {
	p := newProject(t)
	p.turn("t1", p.everyKindOfChange)

	dry := p.undo(Ask{Turns: 1, DryRun: true})
	if !dry.DryRun || len(dry.Restored) != 4 || !slices.Equal(dry.Removed, []string{"made/deep/new.txt"}) {
		t.Errorf("dry run said %+v, want four restored and made/deep/new.txt removed", dry)
	}
	p.want("kept.txt", "the turn wrote this\n")

	report := p.undo(Ask{Turns: 1})
	if !slices.Equal(report.Turns, []string{"t1"}) || len(report.Refused) != 0 {
		t.Errorf("undo said %+v", report)
	}
	p.want("crlf.txt", crlfText)
	p.want("binary.dat", binaryText)
	p.want("kept.txt", "edited by the turn\n")
	p.want("gone.txt", "deleted by the turn\n")
	p.want("made/deep/new.txt", "<absent>")
	if _, err := os.Stat(p.path("made")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the folders the turn created are still there: %v", err)
	}
	p.want("ignored.log", "after\n")
	if got := len(p.read("large.bin")); got != largeBytes {
		t.Errorf("the large untracked file was touched: %d bytes left", got)
	}
	if _, err := p.repo.Undo(context.Background(), Ask{Turns: 1}); !errors.As(err, new(NothingRecorded)) {
		t.Errorf("a second undo with nothing left said %v, want NothingRecorded", err)
	}
}

func TestUndoRefusesAFileChangedAfterTheTurnByName(t *testing.T) {
	p := newProject(t)
	p.turn("t1", func() {
		p.write("kept.txt", "the turn wrote this\n")
		p.write("crlf.txt", "the turn wrote this too\n")
	})
	p.write("kept.txt", "a person wrote this after\n")

	report := p.undo(Ask{Turns: 1})
	if len(report.Refused) != 1 || report.Refused[0].Path != "kept.txt" || !slices.Equal(report.Restored, []string{"crlf.txt"}) {
		t.Errorf("undo said %+v, want kept.txt refused and crlf.txt restored", report)
	}
	p.want("kept.txt", "a person wrote this after\n")
	p.want("crlf.txt", crlfText)
}

func TestForceRestoresAFileChangedAfterTheTurn(t *testing.T) {
	p := newProject(t)
	p.turn("t1", func() { p.write("kept.txt", "the turn wrote this\n") })
	p.write("kept.txt", "a background shell wrote this after\n")

	if report := p.undo(Ask{Turns: 1, Force: true}); len(report.Refused) != 0 {
		t.Errorf("a forced undo refused %+v", report.Refused)
	}
	p.want("kept.txt", "edited by the turn\n")
}

func TestUndoTwiceGoesOneTurnFurtherBack(t *testing.T) {
	p := newProject(t)
	p.turn("t1", func() { p.write("kept.txt", "first\n") })
	p.turn("t2", func() { p.write("kept.txt", "second\n") })

	p.undo(Ask{Turns: 1})
	p.want("kept.txt", "first\n")
	if report := p.undo(Ask{Turns: 1}); !slices.Equal(report.Turns, []string{"t1"}) {
		t.Errorf("the second undo undid %v, want t1", report.Turns)
	}
	p.want("kept.txt", "edited by the turn\n")
}

func TestUndoOfTwoTurnsGoesBackToTheFirstStart(t *testing.T) {
	p := newProject(t)
	p.turn("t1", func() { p.write("kept.txt", "first\n") })
	p.turn("t2", func() { p.write("made.txt", "second\n") })

	if report := p.undo(Ask{Turns: 2}); !slices.Equal(report.Turns, []string{"t1", "t2"}) {
		t.Errorf("undo 2 undid %v", report.Turns)
	}
	p.want("kept.txt", "edited by the turn\n")
	p.want("made.txt", "<absent>")
}

func TestUndoSaysHowManyTurnsAreRecorded(t *testing.T) {
	p := newProject(t)
	var nothing NothingRecorded
	if _, err := p.repo.Undo(context.Background(), Ask{Turns: 1}); !errors.As(err, &nothing) || nothing.Recorded != 0 {
		t.Errorf("undo on a fresh session said %v", err)
	}
	p.turn("t1", func() { p.write("kept.txt", "first\n") })
	p.turn("t2", func() { p.write("kept.txt", "second\n") })
	if _, err := p.repo.Undo(context.Background(), Ask{Turns: 3}); !errors.As(err, &nothing) || nothing != (NothingRecorded{Asked: 3, Recorded: 2}) {
		t.Errorf("undo 3 of 2 said %v", err)
	}
	p.want("kept.txt", "second\n")
}

func TestUndoRefusesATurnWithAStartAndNoEnd(t *testing.T) {
	p := newProject(t)
	if err := p.repo.Begin(context.Background(), "running"); err != nil {
		t.Fatal(err)
	}
	p.write("kept.txt", "written mid-turn\n")

	var running StillRunning
	if _, err := p.repo.Undo(context.Background(), Ask{Turns: 1}); !errors.As(err, &running) || running.Turn != "running" {
		t.Errorf("undo of a running turn said %v", err)
	}
	p.want("kept.txt", "written mid-turn\n")
	p.undo(Ask{Turns: 1, Force: true})
	p.want("kept.txt", "edited by the turn\n")
}

func TestAKilledTurnEndsWhereTheNextOneStarts(t *testing.T) {
	p := newProject(t)
	if err := p.repo.Begin(context.Background(), "killed"); err != nil {
		t.Fatal(err)
	}
	p.write("kept.txt", "the killed turn wrote this\n")
	p.turn("t2", func() { p.write("made.txt", "second\n") })

	p.undo(Ask{Turns: 1})
	p.want("made.txt", "<absent>")
	p.want("kept.txt", "the killed turn wrote this\n")
	p.undo(Ask{Turns: 1})
	p.want("kept.txt", "edited by the turn\n")
}

func TestWithoutGitUndoIsUnavailable(t *testing.T) {
	p := newProject(t)
	t.Setenv("PATH", t.TempDir())
	if err := p.repo.Begin(context.Background(), "t1"); !errors.Is(err, ErrNoGit) {
		t.Errorf("begin with no git said %v", err)
	}
	if _, err := p.repo.Undo(context.Background(), Ask{Turns: 1}); !errors.Is(err, ErrNoGit) {
		t.Errorf("undo with no git said %v", err)
	}
}

func TestALockedFileIsReportedAndTheRetryFinishes(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("an open handle blocks a delete only on Windows")
	}
	p := newProject(t)
	p.turn("t1", func() {
		p.write("made.txt", "created by the turn\n")
		p.write("kept.txt", "the turn wrote this\n")
	})
	held, err := os.Open(p.path("made.txt"))
	if err != nil {
		t.Fatal(err)
	}
	report := p.undo(Ask{Turns: 1})
	_ = held.Close()
	if len(report.Refused) != 1 || report.Refused[0].Path != "made.txt" {
		t.Errorf("undo with made.txt held open said %+v", report)
	}
	p.want("kept.txt", "edited by the turn\n")

	retry := p.undo(Ask{Turns: 1})
	if !slices.Equal(retry.Removed, []string{"made.txt"}) || len(retry.Refused)+len(retry.Restored) != 0 {
		t.Errorf("the retry said %+v, want made.txt removed and kept.txt skipped", retry)
	}
	p.want("made.txt", "<absent>")
}
