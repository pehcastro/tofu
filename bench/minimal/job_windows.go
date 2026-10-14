package main

import (
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

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

const (
	mechanism = "job object"
	jobTick   = 100 * time.Nanosecond
)

type cage struct{ job windows.Handle }

func confine(cmd *exec.Cmd, limit limits) (cage, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_SUSPENDED}
	if err := cmd.Start(); err != nil {
		return cage{}, err
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return cage{}, err
	}
	var bounds windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	if limit.MemoryMB > 0 {
		bounds.BasicLimitInformation.LimitFlags |= windows.JOB_OBJECT_LIMIT_JOB_MEMORY
		bounds.JobMemoryLimit = uintptr(limit.MemoryMB) * mebibyte
	}
	if limit.CPU >= 0 {
		bounds.BasicLimitInformation.LimitFlags |= windows.JOB_OBJECT_LIMIT_AFFINITY
		bounds.BasicLimitInformation.Affinity = 1 << limit.CPU
	}
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&bounds)), uint32(unsafe.Sizeof(bounds))); err != nil {
		return cage{}, err
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.PROCESS_SUSPEND_RESUME, false, uint32(cmd.Process.Pid))
	if err != nil {
		return cage{}, err
	}
	defer func() { _ = windows.CloseHandle(process) }()
	if err := windows.AssignProcessToJobObject(job, process); err != nil {
		return cage{}, err
	}
	if status, _, _ := resumeProcess.Call(uintptr(process)); status != 0 {
		return cage{}, windows.NTStatus(status)
	}
	return cage{job: job}, nil
}

func imageName(process windows.Handle) string {
	path := make([]uint16, windows.MAX_PATH)
	size := uint32(len(path))
	if windows.QueryFullProcessImageName(process, 0, &path[0], &size) != nil {
		return "unknown"
	}
	return filepath.Base(windows.UTF16ToString(path[:size]))
}

func (c cage) sample() reading {
	now := reading{rss: map[string]uint64{}, processes: map[string]int{}}
	var list memberList
	if err := windows.QueryInformationJobObject(c.job, windows.JobObjectBasicProcessIdList, uintptr(unsafe.Pointer(&list)), uint32(unsafe.Sizeof(list)), nil); err != nil {
		return now
	}
	for _, id := range list.ids[:list.listed] {
		process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(id))
		if err != nil {
			continue
		}
		counters := processMemoryCounters{size: uint32(unsafe.Sizeof(processMemoryCounters{}))}
		if ok, _, _ := processMemory.Call(uintptr(process), uintptr(unsafe.Pointer(&counters)), uintptr(counters.size)); ok != 0 {
			name := imageName(process)
			now.processes[name]++
			now.rss[name] += uint64(counters.workingSetSize)
			now.chargedBytes += uint64(counters.pagefileUsage)
			now.privateBytes += uint64(counters.pagefileUsage)
		}
		_ = windows.CloseHandle(process)
	}
	return now
}

func (c cage) finish() finished {
	var accounting jobAccounting
	_ = windows.QueryInformationJobObject(c.job, windows.JobObjectBasicAccountingInformation, uintptr(unsafe.Pointer(&accounting)), uint32(unsafe.Sizeof(accounting)), nil)
	var bounds windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	_ = windows.QueryInformationJobObject(c.job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&bounds)), uint32(unsafe.Sizeof(bounds)), nil)
	_ = windows.TerminateJobObject(c.job, 1)
	_ = windows.CloseHandle(c.job)
	return finished{cpu: time.Duration(accounting.totalUserTime+accounting.totalKernelTime) * jobTick, peakBytes: uint64(bounds.PeakJobMemoryUsed)}
}
