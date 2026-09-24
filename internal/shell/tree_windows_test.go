//go:build windows

package shell

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const backgroundDescendants = 20

func childrenOutsideTheJob(t *testing.T, job windows.Handle, pid int) (children int, strays []windows.Handle) {
	t.Helper()
	active, err := activeProcesses(job)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := memberIDs(job, active)
	if err != nil {
		t.Fatal(err)
	}
	member := map[uint32]bool{}
	for _, one := range ids {
		member[one] = true
	}
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = windows.CloseHandle(snapshot) }()
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err := windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		if entry.ParentProcessID != uint32(pid) {
			continue
		}
		children++
		if member[entry.ProcessID] {
			continue
		}
		handle, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_TERMINATE, false, entry.ProcessID)
		if err != nil {
			continue
		}
		strays = append(strays, handle)
	}
	return children, strays
}

const childRegistrationDeadline = 500 * time.Millisecond
const childRegistrationAttempts = 5

func childrenOutsideTheJobEventually(t *testing.T, job windows.Handle, pid int) (children int, strays []windows.Handle) {
	t.Helper()
	started := time.Now()
	deadline := started.Add(childRegistrationDeadline)
	for {
		children, strays = childrenOutsideTheJob(t, job, pid)
		if children > 0 {
			t.Logf("the spawned child registered %v after the shell announced it was listening", time.Since(started))
			return children, strays
		}
		if time.Now().After(deadline) {
			return children, strays
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func attemptShellUnderAJob(t *testing.T) (job windows.Handle, cmd *exec.Cmd, children int, strays []windows.Handle, ok bool) {
	t.Helper()
	shell, err := posixShell()
	if err != nil {
		t.Fatal(err)
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()
	cmd = exec.Command(shell, "-c", "sleep 30 & echo listening on :3000; wait")
	cmd.Dir = t.TempDir()
	cmd.Stdout, cmd.Stderr = writer, writer
	if err := spawnSuspended(cmd); err != nil {
		t.Fatal(err)
	}
	job, err = adoptIntoJob(cmd.Process.Pid)
	if err != nil {
		_ = cmd.Process.Kill()
		t.Fatal(err)
	}
	if err := resumeSuspended(cmd.Process.Pid); err != nil {
		_ = cmd.Process.Kill()
		_ = windows.CloseHandle(job)
		t.Fatal(err)
	}
	_ = writer.Close()
	if _, err := bufio.NewReader(reader).ReadString('\n'); err != nil {
		t.Fatalf("the shell never announced it was listening: %v", err)
	}
	children, strays = childrenOutsideTheJobEventually(t, job, cmd.Process.Pid)
	if children > 0 {
		return job, cmd, children, strays, true
	}
	endShellTree(cmd, job)
	return 0, nil, 0, nil, false
}

func endShellTree(cmd *exec.Cmd, job windows.Handle) {
	_ = killTree(cmd.Process.Pid)
	_ = cmd.Wait()
	_ = windows.CloseHandle(job)
}

func TestEveryProcessTheShellSpawnsIsAJobMemberBecauseItCannotRunBeforeItIsAdopted(t *testing.T) {
	var job windows.Handle
	var cmd *exec.Cmd
	var children int
	var strays []windows.Handle
	ok := false
	for attempt := 1; attempt <= childRegistrationAttempts && !ok; attempt++ {
		job, cmd, children, strays, ok = attemptShellUnderAJob(t)
		if !ok {
			t.Logf("attempt %d: the shell announced it was listening but no child registered within %v, retrying with a fresh shell", attempt, childRegistrationDeadline)
		}
	}
	if !ok {
		t.Fatal("the shell spawned nothing, so nothing proves a spawn could have escaped")
	}
	defer endShellTree(cmd, job)
	defer func() {
		for _, stray := range strays {
			_ = windows.TerminateProcess(stray, 1)
			_ = windows.CloseHandle(stray)
		}
	}()
	if len(strays) > 0 {
		t.Errorf("%d of the %d processes the shell spawned are outside its job", len(strays), children)
	}
	if err := killTree(cmd.Process.Pid); err != nil {
		t.Fatal(err)
	}
	running := 0
	for _, stray := range strays {
		if state, err := windows.WaitForSingleObject(stray, 0); err != nil || state != windows.WAIT_OBJECT_0 {
			running++
		}
	}
	if running > 0 {
		t.Errorf("%d processes outside the job were still running when Kill returned", running)
	}
}

func TestKillReturnsOnlyAfterEveryProcessOfTheTreeHasExited(t *testing.T) {
	r := registry(t)
	started, err := r.Start(t.TempDir(), "dev-server", strings.Repeat("sleep 30 & ", backgroundDescendants)+"echo listening on :3000; wait")
	if err != nil {
		t.Fatal(err)
	}
	job, err := openJobByName(started.PID, jobAccessQuery)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = windows.CloseHandle(job) }()
	waitForDescendants(t, r, "dev-server")
	active, err := activeProcesses(job)
	if err != nil {
		t.Fatal(err)
	}
	handles, err := memberHandles(job, active)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, handle := range handles {
			_ = windows.CloseHandle(handle)
		}
	}()
	if len(handles) <= backgroundDescendants {
		t.Fatalf("the job holds %d processes, so the descendants are not in the tree being killed", len(handles))
	}
	if err := r.Kill("dev-server"); err != nil {
		t.Fatal(err)
	}
	running := 0
	for _, handle := range handles {
		state, err := windows.WaitForSingleObject(handle, 0)
		if err != nil || state != windows.WAIT_OBJECT_0 {
			running++
		}
	}
	if running > 0 {
		t.Errorf("%d of %d processes in the tree were still running when Kill returned", running, len(handles))
	}
}

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

const orphanProbeVar = "TOFU_SHELL_ORPHAN_PROBE"

func orphanProbe(dir string) {
	started, err := OpenAt(dir).Start(dir, "dev-server", "ping -n 600 127.0.0.1")
	if err != nil {
		fmt.Println("probe:", err)
		os.Exit(1)
	}
	fmt.Println(started.PID)
	_, _ = io.Copy(io.Discard, os.Stdin)
	os.Exit(0)
}

func treeMembersOnceADescendantJoined(t *testing.T, pid int) []windows.Handle {
	t.Helper()
	deadline := time.Now().Add(childRegistrationDeadline)
	for {
		job, err := openJobByName(pid, jobAccessQuery)
		if err != nil {
			t.Fatal(err)
		}
		active, err := activeProcesses(job)
		var ids []uint32
		if err == nil && active > 0 {
			ids, err = memberIDs(job, active)
		}
		_ = windows.CloseHandle(job)
		if err != nil {
			t.Fatal(err)
		}
		if len(ids) > 1 {
			handles := make([]windows.Handle, 0, len(ids))
			for _, id := range ids {
				handle, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_TERMINATE, false, id)
				if err != nil {
					continue
				}
				handles = append(handles, handle)
			}
			return handles
		}
		if time.Now().After(deadline) {
			t.Fatalf("the probe's tree holds %d processes, so no descendant joined and nothing proves a descendant dies too", len(ids))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestABackgroundTreeDoesNotOutliveTheProgramThatStartedIt(t *testing.T) {
	if dir := os.Getenv(orphanProbeVar); dir != "" {
		orphanProbe(dir)
	}
	probe := exec.Command(os.Args[0], "-test.run", t.Name())
	probe.Env = append(os.Environ(), orphanProbeVar+"="+t.TempDir())
	stdin, err := probe.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := probe.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := probe.Start(); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatalf("the probe printed no pid: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatalf("the probe printed %q instead of a pid", strings.TrimSpace(line))
	}
	handles := treeMembersOnceADescendantJoined(t, pid)
	defer func() {
		for _, handle := range handles {
			if state, err := windows.WaitForSingleObject(handle, 0); err != nil || state != windows.WAIT_OBJECT_0 {
				_ = windows.TerminateProcess(handle, 1)
			}
			_ = windows.CloseHandle(handle)
		}
	}()
	_ = stdin.Close()
	if err := probe.Wait(); err != nil {
		t.Fatalf("the probe did not exit cleanly: %v", err)
	}
	if err := waitMembersExited(handles, time.Now().Add(killWait)); err != nil {
		t.Errorf("the probe exited and the %d processes of its background tree are still running: %v", len(handles), err)
	}
}

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
