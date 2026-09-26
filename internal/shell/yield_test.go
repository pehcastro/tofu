package shell

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func shellCommand(t *testing.T, dir, command string) *exec.Cmd {
	t.Helper()
	choice, err := Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	if looksLikeWSLPath(choice.Path) {
		t.Fatalf("the default shell resolved to %s, which is wsl", choice.Path)
	}
	cmd := exec.Command(choice.Path, "-c", command)
	cmd.Dir = dir
	return cmd
}

func TestAShortCommandReturnsItsOutputAndCodeAndLeavesNothingBehind(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "shells")
	registry := OpenAt(dir)
	ran, output, err := registry.Yield(context.Background(), shellCommand(t, t.TempDir(), "echo short; exit 3"), "echo short; exit 3", "", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if ran.State != Exited || ran.ExitCode == nil || *ran.ExitCode != 3 || strings.TrimSpace(output) != "short" {
		t.Fatalf("a command ending inside the yield came back %+v with output %q, want exited 3 with short", ran, output)
	}
	left, _ := os.ReadDir(dir)
	if len(left) != 0 {
		t.Fatalf("a command ending inside the yield left %d files in the registry", len(left))
	}
}

func TestAProcessStillRunningAtTheYieldIsKeptUnderAFreshNameAcrossLaunches(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "shells")
	first := OpenAt(dir)
	ran, output, err := first.Yield(context.Background(), shellCommand(t, t.TempDir(), "echo up; sleep 30"), "echo up; sleep 30", "", 500*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Kill(ran.Name) })
	if ran.State != Running || ran.Name == "" || strings.TrimSpace(output) != "up" {
		t.Fatalf("a process still running at the yield came back %+v with output %q", ran, output)
	}
	next := OpenAt(dir)
	again, _, err := next.Yield(context.Background(), shellCommand(t, t.TempDir(), "sleep 30"), "sleep 30", "", 500*time.Millisecond)
	if err != nil {
		t.Fatalf("a second launch could not start beside the first launch's running process: %v", err)
	}
	t.Cleanup(func() { _ = next.Kill(again.Name) })
	if again.Name == ran.Name {
		t.Fatalf("the second launch reused the running name %s", ran.Name)
	}
}

func TestAStoppedTurnKeepsTheProcessRatherThanWaitingOutTheYield(t *testing.T) {
	registry := OpenAt(filepath.Join(t.TempDir(), "shells"))
	stopped, stop := context.WithCancel(context.Background())
	stop()
	started := time.Now()
	ran, _, err := registry.Yield(stopped, shellCommand(t, t.TempDir(), "sleep 30"), "sleep 30", "", 20*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Kill(ran.Name) })
	if ran.State != Running || time.Since(started) > 10*time.Second {
		t.Fatalf("a stopped turn came back %+v after %v", ran, time.Since(started))
	}
}

func TestPruneDropsWhatExitedAndKeepsWhatRuns(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "shells")
	registry := OpenAt(dir)
	running, _, err := registry.Yield(context.Background(), shellCommand(t, t.TempDir(), "sleep 30"), "sleep 30", "", 500*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Kill(running.Name) })
	ended, _, err := registry.Yield(context.Background(), shellCommand(t, t.TempDir(), "sleep 2"), "sleep 2", "", 500*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for entry, _ := registry.Read(ended.Name); entry.State == Running && time.Now().Before(deadline); entry, _ = registry.Read(ended.Name) {
		time.Sleep(50 * time.Millisecond)
	}
	if err := OpenAt(dir).Prune(); err != nil {
		t.Fatal(err)
	}
	listed, _ := OpenAt(dir).List()
	if len(listed) != 1 || listed[0].Name != running.Name {
		t.Fatalf("after the prune the registry lists %+v, want only %s", listed, running.Name)
	}
	if _, err := os.Stat(filepath.Join(dir, ended.Name+logSuffix)); !os.IsNotExist(err) {
		t.Fatalf("the pruned entry's log is still there: %v", err)
	}
}
