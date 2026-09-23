package shell

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func registry(t *testing.T) *Registry {
	t.Helper()
	return OpenAt(filepath.Join(t.TempDir(), "shells"))
}

func waitForExit(t *testing.T, r *Registry, name string) Shell {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		entry, err := r.Read(name)
		if err == nil && entry.State == Exited {
			return entry
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%q never reached state %q", name, Exited)
	return Shell{}
}

func TestADevServerABuildAndATestRunEachRegisterAsAShell(t *testing.T) {
	r := registry(t)
	cases := []struct {
		name    string
		command string
	}{
		{"dev-server", "for i in 1 2 3; do echo listening; sleep 0.05; done"},
		{"build", "echo compiling && echo done"},
		{"test-run", "echo ok 3 passed"},
	}
	for _, one := range cases {
		if _, err := r.Start(t.TempDir(), one.name, one.command); err != nil {
			t.Fatalf("starting %q: %v", one.name, err)
		}
	}
	shells, err := r.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(shells) != len(cases) {
		t.Fatalf("registered %d processes, want %d\n%+v", len(shells), len(cases), shells)
	}
	for _, one := range cases {
		waitForExit(t, r, one.name)
	}
}

func TestAProcessThatExitsOnItsOwnIsMarkedExitedWithItsOutput(t *testing.T) {
	r := registry(t)
	if _, err := r.Start(t.TempDir(), "build", "echo compiling && echo done"); err != nil {
		t.Fatal(err)
	}
	entry := waitForExit(t, r, "build")
	if entry.ExitCode == nil || *entry.ExitCode != 0 {
		t.Errorf("exit code = %v, want 0", entry.ExitCode)
	}
	log, err := r.Tail("build", DefaultTail)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(log, "compiling") || !strings.Contains(log, "done") {
		t.Errorf("log = %q, missing the command's own output", log)
	}
}

func TestKillingARunningProcessStopsItAndMarksItKilled(t *testing.T) {
	r := registry(t)
	if _, err := r.Start(t.TempDir(), "dev-server", "sleep 30"); err != nil {
		t.Fatal(err)
	}
	if err := r.Kill("dev-server"); err != nil {
		t.Fatal(err)
	}
	entry, err := r.Read("dev-server")
	if err != nil {
		t.Fatal(err)
	}
	if entry.State != Killed {
		t.Errorf("state = %q, want %q", entry.State, Killed)
	}
}

func TestKillingATwiceEndedProcessIsNotOverwrittenByItsOwnExit(t *testing.T) {
	r := registry(t)
	if _, err := r.Start(t.TempDir(), "dev-server", "sleep 30"); err != nil {
		t.Fatal(err)
	}
	if err := r.Kill("dev-server"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	entry, err := r.Read("dev-server")
	if err != nil {
		t.Fatal(err)
	}
	if entry.State != Killed {
		t.Errorf("the background waiter overwrote a kill with %q", entry.State)
	}
}

func waitForDescendants(t *testing.T, r *Registry, name string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		log, err := r.Tail(name, DefaultTail)
		if err == nil && strings.Contains(log, "listening") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%q never wrote its log, so nothing proves its descendants had started", name)
}

func TestKillLeavesNothingHoldingTheLogFileOpen(t *testing.T) {
	r := registry(t)
	if _, err := r.Start(t.TempDir(), "dev-server", "sleep 30 & echo listening on :3000; wait"); err != nil {
		t.Fatal(err)
	}
	waitForDescendants(t, r, "dev-server")
	if err := r.Kill("dev-server"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(r.logPath("dev-server")); err != nil {
		t.Fatalf("a descendant still held the log when Kill returned: %v", err)
	}
}

func TestStartingTheSameNameTwiceWhileRunningFails(t *testing.T) {
	r := registry(t)
	if _, err := r.Start(t.TempDir(), "dev-server", "sleep 30"); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Kill("dev-server") }()
	if _, err := r.Start(t.TempDir(), "dev-server", "echo again"); err == nil {
		t.Fatal("starting a second process under a running name did not fail")
	}
}

func rowLeftBehindByATofuThatExited(t *testing.T) *Registry {
	t.Helper()
	owner := registry(t)
	started, err := owner.Start(t.TempDir(), "dev-server", "sleep 30")
	if err != nil {
		t.Fatal(err)
	}
	left := registry(t)
	if err := os.MkdirAll(left.dir, dirMode); err != nil {
		t.Fatal(err)
	}
	left.mu.Lock()
	writeErr := left.writeLocked(started)
	left.mu.Unlock()
	if writeErr != nil {
		t.Fatal(writeErr)
	}
	if entry, err := left.Read("dev-server"); err != nil || entry.State != Running {
		t.Fatalf("a row whose tree is alive read as %q, %v", entry.State, err)
	}
	if err := killTree(started.PID); err != nil {
		t.Fatal(err)
	}
	waitForExit(t, owner, "dev-server")
	return left
}

func TestARowWhoseProcessIsGoneReadsAsFinished(t *testing.T) {
	left := rowLeftBehindByATofuThatExited(t)
	shells, err := left.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(shells) != 1 {
		t.Fatalf("listed %d rows, want 1\n%+v", len(shells), shells)
	}
	if shells[0].State == Running {
		t.Errorf("a row whose tree is gone still lists as %q", shells[0].State)
	}
	if shells[0].ExitCode != nil {
		t.Errorf("a row nobody waited on reports exit code %d, and nothing read one", *shells[0].ExitCode)
	}
}

func TestANameHeldByAFinishedRowCanBeStartedAgain(t *testing.T) {
	left := rowLeftBehindByATofuThatExited(t)
	if _, err := left.Start(t.TempDir(), "dev-server", "echo again"); err != nil {
		t.Fatalf("starting a name held by a row whose tree is gone: %v", err)
	}
	waitForExit(t, left, "dev-server")
}

func TestKillingATreeThatIsAlreadyGoneReportsItInsteadOfSucceeding(t *testing.T) {
	r := registry(t)
	started, err := r.Start(t.TempDir(), "dev-server", "sleep 30")
	if err != nil {
		t.Fatal(err)
	}
	if err := killTree(started.PID); err != nil {
		t.Fatalf("killing a live tree: %v", err)
	}
	waitForExit(t, r, "dev-server")
	if err := killTree(started.PID); !errors.Is(err, ErrTreeGone) {
		t.Fatalf("killing a tree that is gone returned %v, want %v", err, ErrTreeGone)
	}
}

const endingProcessRounds = 20

func TestAFinishedRowKeepsItsExitCodeAgainstAReaderPollingAsItEnds(t *testing.T) {
	r := registry(t)
	for round := range endingProcessRounds {
		name := "build" + strconv.Itoa(round)
		if _, err := r.Start(t.TempDir(), name, "echo compiling"); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(5 * time.Second)
		for {
			entry, err := r.Read(name)
			if err != nil {
				t.Fatal(err)
			}
			if entry.State == Exited {
				if entry.ExitCode == nil {
					t.Fatalf("round %d read %q as finished with no exit code while its waiter was still writing one", round, name)
				}
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("round %d: %q never finished", round, name)
			}
		}
	}
}

func TestKillingWhatIsNotRunningFails(t *testing.T) {
	r := registry(t)
	if _, err := r.Start(t.TempDir(), "build", "echo compiling"); err != nil {
		t.Fatal(err)
	}
	waitForExit(t, r, "build")
	if err := r.Kill("build"); err == nil {
		t.Fatal("killing an already-exited process did not fail")
	}
}
