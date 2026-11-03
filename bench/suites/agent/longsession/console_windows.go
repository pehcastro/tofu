//go:build windows

package main

import (
	"io"
	"os"
	"strings"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

const ctrlC = 0x03

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

var processMemory = windows.NewLazySystemDLL("kernel32.dll").NewProc("K32GetProcessMemoryInfo")

type console struct {
	in      *os.File
	out     *os.File
	pseudo  windows.Handle
	process windows.Handle
	job     windows.Handle
}

func launch(command []string, dir string, env []string) (*console, error) {
	var inRead, inWrite, outRead, outWrite, pseudo windows.Handle
	if err := windows.CreatePipe(&inRead, &inWrite, nil, 0); err != nil {
		return nil, err
	}
	defer func() { _ = windows.CloseHandle(inRead) }()
	if err := windows.CreatePipe(&outRead, &outWrite, nil, 0); err != nil {
		return nil, err
	}
	defer func() { _ = windows.CloseHandle(outWrite) }()
	if err := windows.CreatePseudoConsole(windows.Coord{X: screenColumns, Y: screenRows}, inRead, outWrite, 0, &pseudo); err != nil {
		return nil, err
	}
	attributes, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return nil, err
	}
	defer attributes.Delete()
	if err := attributes.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, *(*unsafe.Pointer)(unsafe.Pointer(&pseudo)), unsafe.Sizeof(pseudo)); err != nil {
		return nil, err
	}
	info := windows.StartupInfoEx{StartupInfo: windows.StartupInfo{Flags: windows.STARTF_USESTDHANDLES}, ProcThreadAttributeList: attributes.List()}
	info.Cb = uint32(unsafe.Sizeof(info))
	line, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(command))
	if err != nil {
		return nil, err
	}
	block := utf16.Encode([]rune(strings.Join(env, "\x00") + "\x00\x00"))
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		return nil, err
	}
	var created windows.ProcessInformation
	flags := uint32(windows.CREATE_SUSPENDED | windows.CREATE_UNICODE_ENVIRONMENT | windows.EXTENDED_STARTUPINFO_PRESENT)
	if err := windows.CreateProcess(nil, line, nil, nil, false, flags, &block[0], windows.StringToUTF16Ptr(dir), &info.StartupInfo, &created); err != nil {
		return nil, err
	}
	defer func() { _ = windows.CloseHandle(created.Thread) }()
	if err := windows.AssignProcessToJobObject(job, created.Process); err != nil {
		return nil, err
	}
	if _, err := windows.ResumeThread(created.Thread); err != nil {
		return nil, err
	}
	return &console{in: os.NewFile(uintptr(inWrite), "console-in"), out: os.NewFile(uintptr(outRead), "console-out"), pseudo: pseudo, process: created.Process, job: job}, nil
}

func (c *console) drain(into io.Writer) { _, _ = io.Copy(into, c.out) }

func (c *console) Write(p []byte) (int, error) { return c.in.Write(p) }

func (c *console) read() (reading, error) { return readProcess(c.process) }

func readPID(pid int) (reading, error) {
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return reading{}, err
	}
	defer func() { _ = windows.CloseHandle(process) }()
	return readProcess(process)
}

func readProcess(process windows.Handle) (reading, error) {
	counters := memoryCounters{size: uint32(unsafe.Sizeof(memoryCounters{}))}
	if ok, _, err := processMemory.Call(uintptr(process), uintptr(unsafe.Pointer(&counters)), uintptr(counters.size)); ok == 0 {
		return reading{}, err
	}
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(process, &created, &exited, &kernel, &user); err != nil {
		return reading{}, err
	}
	ticks := func(at windows.Filetime) float64 {
		return float64(uint64(at.HighDateTime)<<32 | uint64(at.LowDateTime))
	}
	return reading{at: time.Now(), workingMB: float64(counters.workingSet) / mebibyte, privateMB: float64(counters.privateUsage) / mebibyte,
		cpuSeconds: (ticks(kernel) + ticks(user)) / 1e7}, nil
}

func (c *console) exited(within time.Duration) bool {
	event, err := windows.WaitForSingleObject(c.process, uint32(within.Milliseconds()))
	return err == nil && event == windows.WAIT_OBJECT_0
}

func (c *console) quit(within time.Duration) bool {
	for range quitPresses {
		_, _ = c.Write([]byte{ctrlC})
		if c.exited(2 * time.Second) {
			return true
		}
	}
	return c.exited(within)
}

func (c *console) close() {
	_ = windows.TerminateJobObject(c.job, 1)
	c.exited(5 * time.Second)
	windows.ClosePseudoConsole(c.pseudo)
	_, _, _, _ = c.in.Close(), c.out.Close(), windows.CloseHandle(c.process), windows.CloseHandle(c.job)
}
