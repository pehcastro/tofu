package fs

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLockASecondTakerIsToldHeldAndCanStillReadTheHolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "machine.lock")
	first, held, err := TryLock(path)
	if err != nil || held {
		t.Fatalf("the first taker got held %v, %v", held, err)
	}
	t.Cleanup(func() { _ = Unlock(first) })
	if _, err := first.WriteString("4242"); err != nil {
		t.Fatal(err)
	}
	second, held, err := TryLock(path)
	if err != nil || !held || second != nil {
		t.Fatalf("the second taker got %v, held %v, %v, want held and no file", second, held, err)
	}
	if body, err := os.ReadFile(path); err != nil || string(body) != "4242" {
		t.Fatalf("a reader of a held lock read %q, %v, want the holder's 4242", body, err)
	}
}

func TestLockAnUnlockLeavesNoFileAndACrashedHolderLeavesOneTheNextTakerTakes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "machine.lock")
	first, _, err := TryLock(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Unlock(first); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("after an unlock the lock file is still there: %v", err)
	}
	crashed, _, err := TryLock(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := crashed.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("a holder that died without unlocking left no file, so a crash and an unlock look alike: %v", err)
	}
	next, held, err := TryLock(path)
	if err != nil || held {
		t.Fatalf("the lock of a crashed holder stayed held %v, %v", held, err)
	}
	_ = Unlock(next)
}

func TestLockInAMissingFolderIsAnErrorAndNotHeld(t *testing.T) {
	file, held, err := TryLock(filepath.Join(t.TempDir(), "gone", "machine.lock"))
	if err == nil || held || file != nil {
		t.Fatalf("a lock in a missing folder gave %v, held %v, %v, want an error", file, held, err)
	}
}

func TestLockALateRemoveByTheLastHolderCannotDeleteTheNextHoldersFile(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("on Unix the holder removes before it closes, and the same-file check covers the racer")
	}
	path := filepath.Join(t.TempDir(), "machine.lock")
	last, _, err := TryLock(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = last.Close()
	next, held, err := TryLock(path)
	if err != nil || held {
		t.Fatalf("the next holder got held %v, %v", held, err)
	}
	t.Cleanup(func() { _ = Unlock(next) })
	if err := os.Remove(path); err == nil {
		t.Fatal("the last holder's late remove deleted the lock the next holder has")
	}
	if again, held, _ := TryLock(path); !held {
		_ = Unlock(again)
		t.Fatal("after the late remove a third taker got the lock the next holder has")
	}
}
