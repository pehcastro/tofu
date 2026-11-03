package main

import (
	"os"
	"os/exec"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const treeMethod = "A job object holds the whole tree from its first instruction: RSS is the sum of every live member's working set, CPU is the job's user plus kernel time including members that already exited, commit is the job's own peak"

type processMemoryCounters struct {
	size                       uint32
	pageFaultCount             uint32
	peakWorkingSetSize         uintptr
	workingSetSize             uintptr
	quotaPeakPagedPoolUsage    uintptr
	quotaPagedPoolUsage        uintptr
	quotaPeakNonPagedPoolUsage uintptr
	quotaNonPagedPoolUsage     uintptr
	pagefileUsage              uintptr
	peakPagefileUsage          uintptr
}

type jobAccounting struct {
	totalUserTime             int64
	totalKernelTime           int64
	thisPeriodTotalUserTime   int64
	thisPeriodTotalKernelTime int64
	totalPageFaultCount       uint32
	totalProcesses            uint32
	activeProcesses           uint32
	totalTerminatedProcesses  uint32
}

type memberList struct {
	assigned uint32
	listed   uint32
	ids      [4096]uintptr
}

var (
	processMemory = windows.NewLazySystemDLL("kernel32.dll").NewProc("K32GetProcessMemoryInfo")
	resumeProcess = windows.NewLazySystemDLL("ntdll.dll").NewProc("NtResumeProcess")
)

type tree struct{ job windows.Handle }

func startTree(cmd *exec.Cmd) (tree, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_SUSPENDED}
	if err := cmd.Start(); err != nil {
		return tree{}, err
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return tree{}, err
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.PROCESS_SUSPEND_RESUME, false, uint32(cmd.Process.Pid))
	if err != nil {
		return tree{}, err
	}
	defer func() { _ = windows.CloseHandle(process) }()
	if err := windows.AssignProcessToJobObject(job, process); err != nil {
		return tree{}, err
	}
	if status, _, _ := resumeProcess.Call(uintptr(process)); status != 0 {
		return tree{}, windows.NTStatus(status)
	}
	return tree{job: job}, nil
}

func (t tree) sample() (rss uint64, members int) {
	var list memberList
	if err := windows.QueryInformationJobObject(t.job, windows.JobObjectBasicProcessIdList, uintptr(unsafe.Pointer(&list)), uint32(unsafe.Sizeof(list)), nil); err != nil {
		return 0, 0
	}
	for _, id := range list.ids[:list.listed] {
		process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(id))
		if err != nil {
			continue
		}
		counters := processMemoryCounters{size: uint32(unsafe.Sizeof(processMemoryCounters{}))}
		if ok, _, _ := processMemory.Call(uintptr(process), uintptr(unsafe.Pointer(&counters)), uintptr(counters.size)); ok != 0 {
			rss += uint64(counters.workingSetSize)
			members++
		}
		_ = windows.CloseHandle(process)
	}
	return rss, members
}

func (t tree) totals(*os.ProcessState) (cpu time.Duration, peakCommit uint64) {
	var accounting jobAccounting
	_ = windows.QueryInformationJobObject(t.job, windows.JobObjectBasicAccountingInformation, uintptr(unsafe.Pointer(&accounting)), uint32(unsafe.Sizeof(accounting)), nil)
	var limits windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	_ = windows.QueryInformationJobObject(t.job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits)), nil)
	return time.Duration(accounting.totalUserTime+accounting.totalKernelTime) * 100, uint64(limits.PeakJobMemoryUsed)
}

func (t tree) close() {
	_ = windows.TerminateJobObject(t.job, 1)
	_ = windows.CloseHandle(t.job)
}
