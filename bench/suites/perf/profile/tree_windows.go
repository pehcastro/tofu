package main

import (
	"errors"
	"os/exec"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const treeMethod = "a Windows job object holds the whole tree from its first instruction and posts each new member to a completion port, where it is opened at once and its handle kept to the end, so its final CPU time and its own peak working set are read even after it exits; its parent comes from its basic information and its command line from NtQueryInformationProcess; every 100 ms the live members' working sets are summed into tree RSS, a page shared by two processes counted in both"

const jobMessageNewProcess = 6

type jobAccounting struct {
	totalUser, totalKernel, periodUser, periodKernel int64
	pageFaults, totalProcesses, activeProcesses      uint32
	terminatedProcesses                              uint32
}

type jobProcessList struct {
	assigned, listed uint32
	pids             [4096]uintptr
}

type jobCompletionPort struct {
	key  uintptr
	port windows.Handle
}

type memoryCounters struct {
	size                             uint32
	pageFaults                       uint32
	peakWorkingSet, workingSet       uintptr
	quotaPeakPaged, quotaPaged       uintptr
	quotaPeakNonPaged, quotaNonPaged uintptr
	pagefileUsage, peakPagefileUsage uintptr
	privateUsage, privateWorkingSet  uintptr
	sharedCommit                     uint64
}

type unicodeString struct {
	length, maximum uint16
	buffer          *uint16
}

var (
	ntResumeProcess = windows.NewLazySystemDLL("ntdll.dll").NewProc("NtResumeProcess")
	processMemory   = windows.NewLazySystemDLL("kernel32.dll").NewProc("K32GetProcessMemoryInfo")
)

type watched struct {
	sync.Mutex
	o       *observation
	handles map[uint32]windows.Handle
}

func observe(cmd *exec.Cmd) (observation, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_SUSPENDED}
	if err := cmd.Start(); err != nil {
		return observation{}, err
	}
	job, port, err := adopt(cmd.Process.Pid)
	if err != nil {
		_ = cmd.Process.Kill()
		return observation{}, err
	}
	defer func() { _ = windows.CloseHandle(job) }()
	defer func() { _ = windows.CloseHandle(port) }()
	started := time.Now()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	w := &watched{o: &observation{root: uint32(cmd.Process.Pid), members: map[uint32]*member{}, kindPeakMB: map[string]float64{}}, handles: map[uint32]windows.Handle{}}
	stop, listening := make(chan struct{}), make(chan struct{})
	go w.listen(port, stop, listening)
	tick := time.NewTicker(sampleEvery)
	defer tick.Stop()
	for waiting := true; waiting; {
		w.sample(job)
		select {
		case <-done:
			waiting = false
		case <-tick.C:
		}
	}
	close(stop)
	<-listening
	o := w.o
	o.WallS = time.Since(started).Seconds()
	o.Exit = cmd.ProcessState.ExitCode()
	var accounting jobAccounting
	_ = windows.QueryInformationJobObject(job, windows.JobObjectBasicAccountingInformation, uintptr(unsafe.Pointer(&accounting)), uint32(unsafe.Sizeof(accounting)), nil)
	var limit windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	_ = windows.QueryInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limit)), uint32(unsafe.Sizeof(limit)), nil)
	o.JobCPUS = float64(accounting.totalUser+accounting.totalKernel) / 1e7
	o.JobProcesses = int(accounting.totalProcesses)
	o.PeakCommitMB = float64(limit.PeakJobMemoryUsed) / mebibyte
	for pid, handle := range w.handles {
		var created, exited, kernel, user windows.Filetime
		if windows.GetProcessTimes(handle, &created, &exited, &kernel, &user) == nil {
			o.members[pid].CPUS = float64(filetimeTicks(kernel)+filetimeTicks(user)) / 1e7
		}
		counters := memoryCounters{size: uint32(unsafe.Sizeof(memoryCounters{}))}
		if ok, _, _ := processMemory.Call(uintptr(handle), uintptr(unsafe.Pointer(&counters)), uintptr(counters.size)); ok != 0 {
			o.members[pid].PeakWorkingSetMB = float64(counters.peakWorkingSet) / mebibyte
			o.members[pid].PeakPrivateMB = float64(counters.peakPagefileUsage) / mebibyte
		}
		_ = windows.CloseHandle(handle)
	}
	return *o, nil
}

func filetimeTicks(f windows.Filetime) int64 {
	return int64(f.HighDateTime)<<32 | int64(f.LowDateTime)
}

func adopt(pid int) (job, port windows.Handle, err error) {
	if job, err = windows.CreateJobObject(nil, nil); err != nil {
		return 0, 0, err
	}
	if port, err = windows.CreateIoCompletionPort(windows.InvalidHandle, 0, 0, 1); err != nil {
		return 0, 0, err
	}
	association := jobCompletionPort{port: port}
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectAssociateCompletionPortInformation, uintptr(unsafe.Pointer(&association)), uint32(unsafe.Sizeof(association))); err != nil {
		return 0, 0, err
	}
	limit := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limit.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limit)), uint32(unsafe.Sizeof(limit))); err != nil {
		return 0, 0, err
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.PROCESS_SUSPEND_RESUME, false, uint32(pid))
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = windows.CloseHandle(process) }()
	if err := windows.AssignProcessToJobObject(job, process); err != nil {
		return 0, 0, err
	}
	if status, _, _ := ntResumeProcess.Call(uintptr(process)); status != 0 {
		return 0, 0, windows.NTStatus(status)
	}
	return job, port, nil
}

func (w *watched) listen(port windows.Handle, stop <-chan struct{}, listening chan<- struct{}) {
	defer close(listening)
	for {
		var message uint32
		var key uintptr
		var overlapped *windows.Overlapped
		err := windows.GetQueuedCompletionStatus(port, &message, &key, &overlapped, uint32(sampleEvery.Milliseconds()))
		switch {
		case errors.Is(err, windows.WAIT_TIMEOUT):
			select {
			case <-stop:
				return
			default:
			}
		case err != nil:
			return
		case message == jobMessageNewProcess:
			w.open(uint32(uintptr(unsafe.Pointer(overlapped))))
		}
	}
}

func (w *watched) open(pid uint32) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return
	}
	var basic windows.PROCESS_BASIC_INFORMATION
	_ = windows.NtQueryInformationProcess(handle, windows.ProcessBasicInformation, unsafe.Pointer(&basic), uint32(unsafe.Sizeof(basic)), nil)
	image := make([]uint16, windows.MAX_LONG_PATH)
	size := uint32(len(image))
	_ = windows.QueryFullProcessImageName(handle, 0, &image[0], &size)
	m := &member{PID: pid, Parent: uint32(basic.InheritedFromUniqueProcessId), Exe: windows.UTF16ToString(image[:size]), CommandLine: commandLine(handle)}
	w.Lock()
	defer w.Unlock()
	w.handles[pid] = handle
	w.o.add(m)
}

func (w *watched) sample(job windows.Handle) {
	var list jobProcessList
	if err := windows.QueryInformationJobObject(job, windows.JobObjectBasicProcessIdList, uintptr(unsafe.Pointer(&list)), uint32(unsafe.Sizeof(list)), nil); err != nil {
		return
	}
	w.Lock()
	defer w.Unlock()
	split := map[string]float64{}
	var total float64
	for _, raw := range list.pids[:list.listed] {
		handle, known := w.handles[uint32(raw)]
		if !known {
			continue
		}
		counters := memoryCounters{size: uint32(unsafe.Sizeof(memoryCounters{}))}
		if ok, _, _ := processMemory.Call(uintptr(handle), uintptr(unsafe.Pointer(&counters)), uintptr(counters.size)); ok != 0 {
			mb := float64(counters.workingSet) / mebibyte
			m := w.o.members[uint32(raw)]
			m.PeakPrivateWSMB = max(m.PeakPrivateWSMB, float64(counters.privateWorkingSet)/mebibyte)
			split[m.Kind] += mb
			total += mb
		}
	}
	w.o.Samples++
	w.o.record(total, split)
}

func commandLine(handle windows.Handle) string {
	buffer := make([]byte, 1<<16)
	if windows.NtQueryInformationProcess(handle, windows.ProcessCommandLineInformation, unsafe.Pointer(&buffer[0]), uint32(len(buffer)), nil) != nil {
		return ""
	}
	text := (*unicodeString)(unsafe.Pointer(&buffer[0]))
	if text.buffer == nil {
		return ""
	}
	return windows.UTF16ToString(unsafe.Slice(text.buffer, text.length/2))
}

func splitCommandLine(line string) []string {
	words, err := windows.DecomposeCommandLine(line)
	if err != nil {
		return nil
	}
	return words
}
