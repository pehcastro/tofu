package main

import (
	"fmt"
	"os"
	"os/exec"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const treeMethod = "a Windows job object holds the whole process tree from its first instruction: CPU is the job's total user plus kernel time, peak tree RSS is the largest sum of every live member's working set sampled every 250 ms, peak commit is the job's PeakJobMemoryUsed"

type jobAccounting struct {
	totalUser, totalKernel, periodUser, periodKernel int64
	pageFaults, totalProcesses, activeProcesses      uint32
	terminatedProcesses                              uint32
}

type jobProcessList struct {
	assigned, listed uint32
	pids             [1024]uintptr
}

type memoryCounters struct {
	size                             uint32
	pageFaults                       uint32
	peakWorkingSet, workingSet       uintptr
	quotaPeakPaged, quotaPaged       uintptr
	quotaPeakNonPaged, quotaNonPaged uintptr
	pagefileUsage, peakPagefileUsage uintptr
}

var (
	ntResumeProcess = windows.NewLazySystemDLL("ntdll.dll").NewProc("NtResumeProcess")
	processMemory   = windows.NewLazySystemDLL("kernel32.dll").NewProc("K32GetProcessMemoryInfo")
)

type tree struct {
	job      windows.Handle
	peakRSS  atomic.Uint64
	stop     chan struct{}
	sampling chan struct{}
}

func startTree(cmd *exec.Cmd) (*tree, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_SUSPENDED}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	job, err := adopt(cmd.Process.Pid)
	if err != nil {
		_ = cmd.Process.Kill()
		return nil, err
	}
	t := &tree{job: job, stop: make(chan struct{}), sampling: make(chan struct{})}
	go t.sample()
	return t, nil
}

func adopt(pid int) (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, err
	}
	limit := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limit.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limit)), uint32(unsafe.Sizeof(limit))); err != nil {
		return 0, err
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.PROCESS_SUSPEND_RESUME, false, uint32(pid))
	if err != nil {
		return 0, err
	}
	defer func() { _ = windows.CloseHandle(process) }()
	if err := windows.AssignProcessToJobObject(job, process); err != nil {
		return 0, err
	}
	if status, _, _ := ntResumeProcess.Call(uintptr(process)); status != 0 {
		return 0, fmt.Errorf("NtResumeProcess status %#x", status)
	}
	return job, nil
}

func (t *tree) sample() {
	defer close(t.sampling)
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		t.peakRSS.Store(max(t.peakRSS.Load(), t.workingSet()))
		select {
		case <-t.stop:
			return
		case <-tick.C:
		}
	}
}

func (t *tree) workingSet() uint64 {
	var list jobProcessList
	if err := windows.QueryInformationJobObject(t.job, windows.JobObjectBasicProcessIdList, uintptr(unsafe.Pointer(&list)), uint32(unsafe.Sizeof(list)), nil); err != nil {
		return 0
	}
	var sum uint64
	for _, pid := range list.pids[:list.listed] {
		process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
		if err != nil {
			continue
		}
		counters := memoryCounters{size: uint32(unsafe.Sizeof(memoryCounters{}))}
		if ok, _, _ := processMemory.Call(uintptr(process), uintptr(unsafe.Pointer(&counters)), uintptr(counters.size)); ok != 0 {
			sum += uint64(counters.workingSet)
		}
		_ = windows.CloseHandle(process)
	}
	return sum
}

func (t *tree) finish(*os.ProcessState) treeUsage {
	close(t.stop)
	<-t.sampling
	var accounting jobAccounting
	_ = windows.QueryInformationJobObject(t.job, windows.JobObjectBasicAccountingInformation, uintptr(unsafe.Pointer(&accounting)), uint32(unsafe.Sizeof(accounting)), nil)
	var limit windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	_ = windows.QueryInformationJobObject(t.job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limit)), uint32(unsafe.Sizeof(limit)), nil)
	_ = windows.CloseHandle(t.job)
	return treeUsage{
		CPUMS:       (accounting.totalUser + accounting.totalKernel) / 10_000,
		PeakTreeRSS: t.peakRSS.Load(),
		PeakCommit:  uint64(limit.PeakJobMemoryUsed),
		Processes:   int(accounting.totalProcesses),
		LeftRunning: int(accounting.activeProcesses),
	}
}
