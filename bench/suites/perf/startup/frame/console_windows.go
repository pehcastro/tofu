//go:build windows

package main

import (
	"os"
	"strings"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

const ctrlC = 0x03

type console struct {
	in      *os.File
	out     *os.File
	pseudo  windows.Handle
	process windows.Handle
	job     windows.Handle
}

func launch(command []string, dir string, env []string, size screenSize) (*console, time.Time, error) {
	var inRead, inWrite, outRead, outWrite, pseudo windows.Handle
	if err := windows.CreatePipe(&inRead, &inWrite, nil, 0); err != nil {
		return nil, time.Time{}, err
	}
	defer func() { _ = windows.CloseHandle(inRead) }()
	if err := windows.CreatePipe(&outRead, &outWrite, nil, 0); err != nil {
		return nil, time.Time{}, err
	}
	defer func() { _ = windows.CloseHandle(outWrite) }()
	if err := windows.CreatePseudoConsole(windows.Coord{X: size.columns, Y: size.rows}, inRead, outWrite, 0, &pseudo); err != nil {
		return nil, time.Time{}, err
	}
	attributes, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return nil, time.Time{}, err
	}
	defer attributes.Delete()
	if err := attributes.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, *(*unsafe.Pointer)(unsafe.Pointer(&pseudo)), unsafe.Sizeof(pseudo)); err != nil {
		return nil, time.Time{}, err
	}
	info := windows.StartupInfoEx{StartupInfo: windows.StartupInfo{Flags: windows.STARTF_USESTDHANDLES}, ProcThreadAttributeList: attributes.List()}
	info.Cb = uint32(unsafe.Sizeof(info))
	line, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(command))
	if err != nil {
		return nil, time.Time{}, err
	}
	block := utf16.Encode([]rune(strings.Join(env, "\x00") + "\x00\x00"))
	job, err := killingJob()
	if err != nil {
		return nil, time.Time{}, err
	}
	var created windows.ProcessInformation
	flags := uint32(windows.CREATE_SUSPENDED | windows.CREATE_UNICODE_ENVIRONMENT | windows.EXTENDED_STARTUPINFO_PRESENT)
	started := time.Now()
	if err := windows.CreateProcess(nil, line, nil, nil, false, flags, &block[0], windows.StringToUTF16Ptr(dir), &info.StartupInfo, &created); err != nil {
		return nil, time.Time{}, err
	}
	defer func() { _ = windows.CloseHandle(created.Thread) }()
	if err := windows.AssignProcessToJobObject(job, created.Process); err != nil {
		return nil, time.Time{}, err
	}
	if _, err := windows.ResumeThread(created.Thread); err != nil {
		return nil, time.Time{}, err
	}
	return &console{in: os.NewFile(uintptr(inWrite), "console-in"), out: os.NewFile(uintptr(outRead), "console-out"),
		pseudo: pseudo, process: created.Process, job: job}, started, nil
}

func killingJob() (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, err
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	_, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits)))
	return job, err
}

func (c *console) Read(p []byte) (int, error) { return c.out.Read(p) }

func (c *console) Write(p []byte) (int, error) { return c.in.Write(p) }

func (c *console) exited(within time.Duration) bool {
	event, err := windows.WaitForSingleObject(c.process, uint32(within.Milliseconds()))
	return err == nil && event == windows.WAIT_OBJECT_0
}

func (c *console) quit() {
	for range 2 {
		_, _ = c.Write([]byte{ctrlC})
		if c.exited(time.Second) {
			return
		}
	}
	c.exited(10 * time.Second)
}

func (c *console) close() {
	_ = windows.TerminateJobObject(c.job, 1)
	c.exited(5 * time.Second)
	windows.ClosePseudoConsole(c.pseudo)
	_, _, _, _ = c.in.Close(), c.out.Close(), windows.CloseHandle(c.process), windows.CloseHandle(c.job)
}
