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
)

const idleSamples = 20

const idleMethod = "the command starts in a 120 by 40 pseudo console, its output kept as screen-<label>-<n>.ansi in the work directory and its input left open, in a fresh home and an empty git project; after the settle time its own working set and private working set are read 20 times 100 ms apart and the median kept, its CPU time over those 2 s divided by the wall time, then the tree is killed with its job object"

func idleOnce(cmd *exec.Cmd, settle time.Duration, screen io.Writer) (idleReading, error) {
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
	cpuBefore, windowStarted := processCPU(process), time.Now()
	for range idleSamples {
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
	idleCPU := 100 * (processCPU(process) - cpuBefore) / time.Since(windowStarted).Seconds()
	_ = windows.TerminateJobObject(job, 1)
	_, _ = windows.WaitForSingleObject(process, windows.INFINITE)
	windows.ClosePseudoConsole(console)
	<-drained
	slices.Sort(working)
	slices.Sort(private)
	return idleReading{WorkingSetMB: working[len(working)/2], PrivateWorkingSetMB: private[len(private)/2], PeakWorkingSetMB: peak, CPUPercent: idleCPU}, nil
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
