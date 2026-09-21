//go:build windows

package shell

import (
	"errors"
	"testing"

	"golang.org/x/sys/windows"
)

func TestAJobNameHeldOpenAfterItsTreeDiedStillReportsGone(t *testing.T) {
	r := registry(t)
	started, err := r.Start(t.TempDir(), "dev-server", "sleep 30")
	if err != nil {
		t.Fatal(err)
	}
	held, err := openJobByName(started.PID, jobAccessQuery)
	if err != nil {
		t.Fatalf("opening the job of a live tree: %v", err)
	}
	defer func() { _ = windows.CloseHandle(held) }()
	if err := killTree(started.PID); err != nil {
		t.Fatalf("killing a live tree: %v", err)
	}
	waitForExit(t, r, "dev-server")
	if _, err := openJobByName(started.PID, jobAccessQuery); err != nil {
		t.Fatalf("the held handle did not keep the name alive: %v", err)
	}
	if err := killTree(started.PID); !errors.Is(err, ErrTreeGone) {
		t.Fatalf("killing a named job with no active process returned %v, want %v", err, ErrTreeGone)
	}
}
