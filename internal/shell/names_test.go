package shell

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"tofu/internal/konst"
)

func TestAFinishedShellOutlivesTheNextLaunchAndItsNameIsNeverTakenAgain(t *testing.T) {
	registry := OpenAt(filepath.Join(t.TempDir(), "shells"))
	now := time.Now()
	stale := now.Add(-(konst.FinishedShellKeptHours + 1) * time.Hour)
	recent := now.Add(-time.Minute)
	code := 0
	if err := os.MkdirAll(registry.dir, dirMode); err != nil {
		t.Fatal(err)
	}
	for _, kept := range []Shell{
		{Name: "bash-1", State: Exited, Started: stale, Ended: &stale, ExitCode: &code},
		{Name: "bash-2", State: Exited, Started: recent, Ended: &recent, ExitCode: &code},
		{Name: "bash-3", State: Killed, Started: recent, Ended: &recent},
	} {
		if err := registry.writeLocked(kept); err != nil {
			t.Fatal(err)
		}
	}
	if err := registry.Prune(); err != nil {
		t.Fatal(err)
	}
	listed, _ := registry.List()
	var names []string
	for _, one := range listed {
		names = append(names, one.Name)
	}
	if len(names) != 2 || names[0] != "bash-2" || names[1] != "bash-3" {
		t.Fatalf("after a launch the registry lists %v, want the two shells that ended a minute ago and not the one past the keep window", names)
	}
	name, logFile, err := registry.claim()
	if err != nil {
		t.Fatal(err)
	}
	_ = logFile.Close()
	if name != "bash-4" {
		t.Fatalf("the next shell is named %s, a name an earlier shell of this project already had", name)
	}
}
