package session

import (
	"bufio"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

const (
	heldSessionVariable = "TOFU_TEST_HELD_SESSIONS"
	heldSessionID       = "turn-held"
)

func TestASecondProcessIsRefusedAndAKilledHolderLeavesNothingHeld(t *testing.T) {
	if dir := os.Getenv(heldSessionVariable); dir != "" {
		if _, err := NewStore(dir).Open(Header{ID: heldSessionID}); err != nil {
			t.Fatal(err)
		}
		_, _ = os.Stdout.WriteString("held\n")
		time.Sleep(time.Minute)
		return
	}
	dir := t.TempDir()
	holder := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$")
	holder.Env = append(os.Environ(), heldSessionVariable+"="+dir)
	out, err := holder.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = holder.Process.Kill() })
	if said, err := bufio.NewReader(out).ReadString('\n'); said != "held\n" {
		t.Fatalf("the holding process said %q, %v", said, err)
	}
	store := NewStore(dir)

	var busy BusyError
	if _, err := store.Open(Header{ID: heldSessionID}); !errors.As(err, &busy) || busy.PID != holder.Process.Pid {
		t.Fatalf("a second writer got %v, want BusyError naming pid %d", err, holder.Process.Pid)
	}
	if err := store.Busy(heldSessionID); !errors.As(err, &busy) {
		t.Fatalf("the probe said %v while another process held the session", err)
	}

	if err := holder.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = holder.Wait()
	lock := filepath.Join(store.Dir(heldSessionID), lockName)
	if _, err := os.Stat(lock); err != nil {
		t.Fatalf("a killed holder leaves its lock file, or this test proves nothing about a stale one: %v", err)
	}
	if err := store.Busy(heldSessionID); err != nil {
		t.Errorf("the probe after the kill said %v", err)
	}
	log, err := store.Open(Header{ID: heldSessionID})
	if err != nil {
		t.Fatalf("the open after the holder was killed failed: %v", err)
	}
	again, err := store.Open(Header{ID: heldSessionID})
	if err != nil {
		t.Fatalf("a second open in the holding process failed: %v", err)
	}
	if err := again.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(lock); err != nil {
		t.Errorf("closing the inner open of the same process released the outer one's lock: %v", err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(lock); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the lock is still there after the only writer closed: %v", err)
	}
}

func TestTwoHoldersInOneProcessShareNoProjectTurn(t *testing.T) {
	dir := t.TempDir()
	first, second := NewStore(dir), NewStore(dir)
	release, err := first.HoldTurn()
	if err != nil {
		t.Fatal(err)
	}
	var busy TurnBusyError
	if again, err := second.HoldTurn(); !errors.As(err, &busy) || busy.PID != os.Getpid() {
		if again != nil {
			_ = again()
		}
		t.Fatalf("a second holder in the process holding the project turn got %v, want TurnBusyError naming pid %d", err, os.Getpid())
	}
	lock := filepath.Join(first.State(), turnLockName)
	if _, err := os.Stat(lock); err != nil {
		t.Fatalf("the refused second holder took the first one's lock with it: %v", err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(lock); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the lock is still there after the only holder released: %v", err)
	}
	next, err := second.HoldTurn()
	if err != nil {
		t.Fatalf("the project turn after the holder released was refused: %v", err)
	}
	if err := next(); err != nil {
		t.Fatal(err)
	}
}
