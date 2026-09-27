//go:build windows

package shell

import (
	"errors"
	"os/exec"
	"runtime"
	"slices"
	"strconv"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

type tree struct{ job windows.Handle }

const (
	jobAccessQuery     = 0x0004
	jobAccessTerminate = 0x0008
)

type jobProcessIDList struct {
	AssignedProcesses uint32
	IDsInList         uint32
	FirstID           uintptr
}

var errTreeStillExiting = errors.New("shell: the process tree had not finished exiting")

func jobName(pid int) (*uint16, error) {
	return windows.UTF16PtrFromString("tofu-shell-" + strconv.Itoa(pid))
}

func adoptIntoJob(pid int, lifetime Lifetime) (windows.Handle, error) {
	name, err := jobName(pid)
	if err != nil {
		return 0, err
	}
	job, err := windows.CreateJobObject(nil, name)
	if err != nil {
		return 0, err
	}
	var limits windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	if lifetime == DiesWithTofu {
		limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	}
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		_ = windows.CloseHandle(job)
		return 0, err
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.PROCESS_DUP_HANDLE, false, uint32(pid))
	if err == nil {
		err = windows.AssignProcessToJobObject(job, process)
		if err == nil && lifetime == OutlivesTofu {
			err = handJobToShell(job, process)
		}
		_ = windows.CloseHandle(process)
	}
	if err != nil {
		_ = windows.CloseHandle(job)
		return 0, err
	}
	return job, nil
}

func handJobToShell(job, shell windows.Handle) error {
	var held windows.Handle
	return windows.DuplicateHandle(windows.CurrentProcess(), job, shell, &held, 0, false, windows.DUPLICATE_SAME_ACCESS)
}

func spawnSuspended(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_SUSPENDED}
	return cmd.Start()
}

func resumeSuspended(pid int) error {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return err
	}
	defer func() { _ = windows.CloseHandle(snapshot) }()
	var entry windows.ThreadEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err := windows.Thread32First(snapshot, &entry); err == nil; err = windows.Thread32Next(snapshot, &entry) {
		if entry.OwnerProcessID != uint32(pid) {
			continue
		}
		thread, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
		if err != nil {
			return err
		}
		_, err = windows.ResumeThread(thread)
		_ = windows.CloseHandle(thread)
		return err
	}
	return errors.New("shell: the suspended shell had no thread to resume")
}

func startTree(cmd *exec.Cmd, lifetime Lifetime) (tree, error) {
	if err := spawnSuspended(cmd); err != nil {
		return tree{}, err
	}
	job, err := adoptIntoJob(cmd.Process.Pid, lifetime)
	if err != nil {
		_ = cmd.Process.Kill()
		return tree{}, err
	}
	if err := resumeSuspended(cmd.Process.Pid); err != nil {
		_ = windows.CloseHandle(job)
		_ = cmd.Process.Kill()
		return tree{}, err
	}
	return tree{job: job}, nil
}

func (t tree) release() { _ = windows.CloseHandle(t.job) }

type jobAccounting struct {
	TotalUserTime             int64
	TotalKernelTime           int64
	ThisPeriodTotalUserTime   int64
	ThisPeriodTotalKernelTime int64
	TotalPageFaultCount       uint32
	TotalProcesses            uint32
	ActiveProcesses           uint32
	TotalTerminatedProcesses  uint32
}

func openJobByName(pid int, access uint32) (windows.Handle, error) {
	name, err := jobName(pid)
	if err != nil {
		return 0, err
	}
	open := windows.NewLazySystemDLL("kernel32.dll").NewProc("OpenJobObjectW")
	handle, _, callErr := open.Call(uintptr(access), 0, uintptr(unsafe.Pointer(name)))
	runtime.KeepAlive(name)
	if handle != 0 {
		return windows.Handle(handle), nil
	}
	if errors.Is(callErr, windows.ERROR_FILE_NOT_FOUND) {
		return 0, ErrTreeGone
	}
	return 0, callErr
}

func activeProcesses(job windows.Handle) (uint32, error) {
	var accounting jobAccounting
	err := windows.QueryInformationJobObject(job, windows.JobObjectBasicAccountingInformation, uintptr(unsafe.Pointer(&accounting)), uint32(unsafe.Sizeof(accounting)), nil)
	return accounting.ActiveProcesses, err
}

func treeAlive(pid int) bool {
	job, err := openJobByName(pid, jobAccessQuery)
	if err != nil {
		return false
	}
	defer func() { _ = windows.CloseHandle(job) }()
	active, err := activeProcesses(job)
	return err == nil && active > 0
}

func memberIDs(job windows.Handle, assigned uint32) ([]uint32, error) {
	list := make([]jobProcessIDList, assigned)
	if err := windows.QueryInformationJobObject(job, windows.JobObjectBasicProcessIdList, uintptr(unsafe.Pointer(&list[0])), uint32(len(list))*uint32(unsafe.Sizeof(list[0])), nil); err != nil {
		return nil, err
	}
	ids := make([]uint32, 0, list[0].IDsInList)
	for _, member := range unsafe.Slice(&list[0].FirstID, list[0].IDsInList) {
		ids = append(ids, uint32(member))
	}
	return ids, nil
}

func treeHas(root, pid int) bool {
	if pid == root {
		return treeAlive(root)
	}
	job, err := openJobByName(root, jobAccessQuery)
	if err != nil {
		return false
	}
	defer func() { _ = windows.CloseHandle(job) }()
	active, err := activeProcesses(job)
	if err != nil || active == 0 {
		return false
	}
	ids, err := memberIDs(job, active)
	return err == nil && slices.Contains(ids, uint32(pid))
}

func memberHandles(job windows.Handle, assigned uint32) ([]windows.Handle, error) {
	ids, err := memberIDs(job, assigned)
	if err != nil {
		return nil, err
	}
	handles := make([]windows.Handle, 0, len(ids))
	for _, id := range ids {
		handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, id)
		if err != nil {
			continue
		}
		handles = append(handles, handle)
	}
	return handles, nil
}

func waitMembersExited(handles []windows.Handle, deadline time.Time) error {
	for _, handle := range handles {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return errTreeStillExiting
		}
		state, err := windows.WaitForSingleObject(handle, uint32(remaining.Milliseconds()))
		if err != nil {
			return err
		}
		if state != windows.WAIT_OBJECT_0 {
			return errTreeStillExiting
		}
	}
	return nil
}

func killTree(pid int) error {
	job, err := openJobByName(pid, jobAccessQuery|jobAccessTerminate)
	if err != nil {
		return err
	}
	defer func() { _ = windows.CloseHandle(job) }()
	active, err := activeProcesses(job)
	if err != nil {
		return err
	}
	if active == 0 {
		return ErrTreeGone
	}
	handles, err := memberHandles(job, active)
	if err != nil {
		return err
	}
	defer func() {
		for _, handle := range handles {
			_ = windows.CloseHandle(handle)
		}
	}()
	if err := windows.TerminateJobObject(job, 1); err != nil {
		return err
	}
	return waitMembersExited(handles, time.Now().Add(killWait))
}
