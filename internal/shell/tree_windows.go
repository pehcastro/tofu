//go:build windows

package shell

import (
	"errors"
	"os/exec"
	"runtime"
	"strconv"
	"unsafe"

	"golang.org/x/sys/windows"
)

type tree struct{ job windows.Handle }

const (
	jobAccessQuery     = 0x0004
	jobAccessTerminate = 0x0008
)

func jobName(pid int) (*uint16, error) {
	return windows.UTF16PtrFromString("tofu-shell-" + strconv.Itoa(pid))
}

func adoptIntoJob(pid int) (windows.Handle, error) {
	name, err := jobName(pid)
	if err != nil {
		return 0, err
	}
	job, err := windows.CreateJobObject(nil, name)
	if err != nil {
		return 0, err
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err == nil {
		err = windows.AssignProcessToJobObject(job, process)
		_ = windows.CloseHandle(process)
	}
	if err != nil {
		_ = windows.CloseHandle(job)
		return 0, err
	}
	return job, nil
}

func startTree(cmd *exec.Cmd) (tree, error) {
	if err := cmd.Start(); err != nil {
		return tree{}, err
	}
	job, err := adoptIntoJob(cmd.Process.Pid)
	if err != nil {
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
	return windows.TerminateJobObject(job, 1)
}
