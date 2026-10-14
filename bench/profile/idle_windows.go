package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const idleMethod = "the command starts in a 120 by 40 pseudo console, its output kept as screen-<label>-<n>.ansi in the work directory and its input left open, in a fresh home and an empty git project; after the settle time its own working set and private working set are read every 100 ms for the window and the median kept, its CPU time over the window divided by the wall time, its threads' context switches and its cycle count read from one NtQuerySystemInformation snapshot at each end of the window, then the tree is killed with its job object"

const (
	systemProcessInformationSize = 0x100
	systemThreadInformationSize  = 0x50
	threadContextSwitchesAt      = 0x40
	statusInfoLengthMismatch     = 0xC0000004
)

type activity struct {
	switches, cycles uint64
	threads          int
}

func processActivity(pid uint32) (activity, error) {
	buffer := make([]byte, 1<<20)
	for {
		err := windows.NtQuerySystemInformation(windows.SystemProcessInformation, unsafe.Pointer(&buffer[0]), uint32(len(buffer)), nil)
		if err == nil {
			break
		}
		if err != windows.NTStatus(statusInfoLengthMismatch) {
			return activity{}, err
		}
		buffer = make([]byte, 2*len(buffer))
	}
	for at := 0; ; {
		info := (*windows.SYSTEM_PROCESS_INFORMATION)(unsafe.Pointer(&buffer[at]))
		if uint32(info.UniqueProcessID) == pid {
			a := activity{cycles: info.CycleTime, threads: int(info.NumberOfThreads)}
			for thread := range a.threads {
				a.switches += uint64(*(*uint32)(unsafe.Pointer(&buffer[at+systemProcessInformationSize+thread*systemThreadInformationSize+threadContextSwitchesAt])))
			}
			return a, nil
		}
		if info.NextEntryOffset == 0 {
			return activity{}, fmt.Errorf("process %d is not in the system process list", pid)
		}
		at += int(info.NextEntryOffset)
	}
}

func idleOnce(cmd *exec.Cmd, settle, window time.Duration, screen io.Writer) (idleReading, error) {
	var inRead, inWrite, outRead, outWrite, console windows.Handle
	if err := windows.CreatePipe(&inRead, &inWrite, nil, 0); err != nil {
		return idleReading{}, err
	}
	defer func() { _ = windows.CloseHandle(inWrite) }()
	if err := windows.CreatePipe(&outRead, &outWrite, nil, 0); err != nil {
		return idleReading{}, err
	}
	err := windows.CreatePseudoConsole(windows.Coord{X: 120, Y: 40}, inRead, outWrite, 0, &console)
	_ = windows.CloseHandle(inRead)
	_ = windows.CloseHandle(outWrite)
	if err != nil {
		return idleReading{}, err
	}
	drained := make(chan struct{})
	go func() {
		_, _ = io.Copy(screen, os.NewFile(uintptr(outRead), "console"))
		close(drained)
	}()
	process, job, err := startInConsole(cmd, console)
	if err != nil {
		windows.ClosePseudoConsole(console)
		return idleReading{}, err
	}
	defer func() { _ = windows.CloseHandle(process) }()
	defer func() { _ = windows.CloseHandle(job) }()
	time.Sleep(settle)
	var working, private []float64
	var peak float64
	pid, _ := windows.GetProcessId(process)
	before, err := processActivity(pid)
	if err != nil {
		return idleReading{}, err
	}
	cpuBefore, windowStarted := processCPU(process), time.Now()
	for range int(window / sampleEvery) {
		if exited, _ := windows.WaitForSingleObject(process, 0); exited == windows.WAIT_OBJECT_0 {
			var code uint32
			_ = windows.GetExitCodeProcess(process, &code)
			windows.ClosePseudoConsole(console)
			return idleReading{}, fmt.Errorf("%s exited with %d before it was read", cmd.Path, code)
		}
		counters := memoryCounters{size: uint32(unsafe.Sizeof(memoryCounters{}))}
		if ok, _, callErr := processMemory.Call(uintptr(process), uintptr(unsafe.Pointer(&counters)), uintptr(counters.size)); ok == 0 {
			return idleReading{}, callErr
		}
		working = append(working, float64(counters.workingSet)/mebibyte)
		private = append(private, float64(counters.privateWorkingSet)/mebibyte)
		peak = float64(counters.peakWorkingSet) / mebibyte
		time.Sleep(sampleEvery)
	}
	after, err := processActivity(pid)
	if err != nil {
		return idleReading{}, err
	}
	seconds := time.Since(windowStarted).Seconds()
	idleCPU := 100 * (processCPU(process) - cpuBefore) / seconds
	_ = windows.TerminateJobObject(job, 1)
	_, _ = windows.WaitForSingleObject(process, windows.INFINITE)
	windows.ClosePseudoConsole(console)
	<-drained
	slices.Sort(working)
	slices.Sort(private)
	return idleReading{WorkingSetMB: working[len(working)/2], PrivateWorkingSetMB: private[len(private)/2], PeakWorkingSetMB: peak, CPUPercent: idleCPU,
		SwitchesPerS: float64(after.switches-before.switches) / seconds, MCyclesPerS: float64(after.cycles-before.cycles) / seconds / 1e6, Threads: after.threads}, nil
}

func nominalMHz() float64 {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `HARDWARE\DESCRIPTION\System\CentralProcessor\0`, registry.QUERY_VALUE)
	if err != nil {
		return 0
	}
	defer func() { _ = key.Close() }()
	mhz, _, err := key.GetIntegerValue("~MHz")
	if err != nil {
		return 0
	}
	return float64(mhz)
}

func processCPU(process windows.Handle) float64 {
	var created, exited, kernel, user windows.Filetime
	_ = windows.GetProcessTimes(process, &created, &exited, &kernel, &user)
	return float64(filetimeTicks(kernel)+filetimeTicks(user)) / 1e7
}

func startInConsole(cmd *exec.Cmd, console windows.Handle) (process, job windows.Handle, err error) {
	attributes, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return 0, 0, err
	}
	defer attributes.Delete()
	if err := attributes.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, *(*unsafe.Pointer)(unsafe.Pointer(&console)), unsafe.Sizeof(console)); err != nil {
		return 0, 0, err
	}
	info := windows.StartupInfoEx{StartupInfo: windows.StartupInfo{Flags: windows.STARTF_USESTDHANDLES}, ProcThreadAttributeList: attributes.List()}
	info.Cb = uint32(unsafe.Sizeof(info))
	if cmd.Err != nil {
		return 0, 0, cmd.Err
	}
	app, _ := windows.UTF16PtrFromString(cmd.Path)
	line, _ := windows.UTF16PtrFromString(windows.ComposeCommandLine(cmd.Args))
	dir, _ := windows.UTF16PtrFromString(cmd.Dir)
	env := utf16.Encode([]rune(strings.Join(cmd.Env, "\x00") + "\x00\x00"))
	var created windows.ProcessInformation
	if err := windows.CreateProcess(app, line, nil, nil, false, windows.CREATE_SUSPENDED|windows.CREATE_UNICODE_ENVIRONMENT|windows.EXTENDED_STARTUPINFO_PRESENT, &env[0], dir, &info.StartupInfo, &created); err != nil {
		return 0, 0, err
	}
	_ = windows.CloseHandle(created.Thread)
	job, port, err := adopt(int(created.ProcessId))
	if err != nil {
		_ = windows.TerminateProcess(created.Process, 1)
		_ = windows.CloseHandle(created.Process)
		return 0, 0, err
	}
	_ = windows.CloseHandle(port)
	return created.Process, job, nil
}
