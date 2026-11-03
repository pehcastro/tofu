package shell

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestTheListOfRunningShellsDoesNotWaitForTheSweepOfEndedShells(t *testing.T) {
	registry := OpenAt(filepath.Join(t.TempDir(), "shells"))
	if err := os.MkdirAll(registry.dir, dirMode); err != nil {
		t.Fatal(err)
	}
	recent, code := time.Now().Add(-time.Minute), 0
	const ended = 3000
	for i := range ended {
		if err := registry.writeLocked(Shell{Name: "bash-" + strconv.Itoa(i), State: Exited, Started: recent, Ended: &recent, ExitCode: &code, Kept: KeptMoved}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := registry.Start(t.TempDir(), "busy", "sleep 60", ""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Kill("busy") })
	swept := make(chan time.Duration)
	started := time.Now()
	go func() {
		_ = registry.Prune()
		swept <- time.Since(started)
	}()
	var slowest time.Duration
	for {
		select {
		case sweep := <-swept:
			if sweep < 100*time.Millisecond {
				t.Skipf("the sweep of %d state files took %s, too short to tell a wait from a read", ended, sweep)
			}
			if slowest > sweep/5 {
				t.Fatalf("listing the running shells waited %s while the sweep of %d ended shells took %s, so the sweep held the registry lock across its file reads", slowest, ended, sweep)
			}
			t.Logf("sweep %s, slowest listing during it %s", sweep, slowest)
			return
		default:
		}
		asked := time.Now()
		if own := registry.Own(); len(own) != 1 || own[0].Name != "busy" {
			t.Fatalf("the registry lists %+v as its own while the sweep runs, want the one running shell busy", own)
		}
		slowest = max(slowest, time.Since(asked))
	}
}
