//go:build windows

package shell

import (
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"

	"tofu/internal/konst"
)

func closeHandles(handles ...windows.Handle) {
	for _, handle := range handles {
		_ = windows.CloseHandle(handle)
	}
}

type device struct{ input, console windows.Handle }

func (d device) write(data []byte) error {
	for len(data) > 0 {
		var written uint32
		if err := windows.WriteFile(d.input, data, &written, nil); err != nil {
			return err
		}
		data = data[written:]
	}
	return nil
}

func (d device) resize(size Size) error {
	return windows.ResizePseudoConsole(d.console, windows.Coord{X: int16(size.cols), Y: int16(size.rows)})
}

func startConsole(cmd *exec.Cmd, out io.Writer, lifetime Lifetime, typed bool) (tree, <-chan error, *terminal, error) {
	var inRead, inWrite, outRead, outWrite, console windows.Handle
	if err := windows.CreatePipe(&inRead, &inWrite, nil, 0); err != nil {
		return tree{}, nil, nil, err
	}
	defer closeHandles(inRead)
	if err := windows.CreatePipe(&outRead, &outWrite, nil, 0); err != nil {
		closeHandles(inWrite)
		return tree{}, nil, nil, err
	}
	defer closeHandles(outWrite)
	err := windows.CreatePseudoConsole(windows.Coord{X: konst.ShellConsoleColumns, Y: konst.ShellConsoleRows}, inRead, outWrite, 0, &console)
	if err != nil {
		closeHandles(inWrite, outRead)
		return tree{}, nil, nil, err
	}
	spawned, err := spawnInConsole(cmd, console, lifetime, typed)
	if err != nil {
		windows.ClosePseudoConsole(console)
		closeHandles(inWrite, outRead)
		return tree{}, nil, nil, err
	}
	sink, term := readerFor(out, typed, device{input: inWrite, console: console})
	drained := make(chan struct{})
	go func() {
		rendered := os.NewFile(uintptr(outRead), "console")
		_, _ = io.Copy(sink, rendered)
		_ = rendered.Close()
		close(drained)
	}()
	waited := make(chan error, 1)
	go func() {
		state, err := cmd.Process.Wait()
		if term != nil {
			term.end()
		}
		windows.ClosePseudoConsole(console)
		select {
		case <-drained:
		case <-time.After(killWait):
		}
		closeHandles(inWrite)
		cmd.ProcessState = state
		if err == nil && !state.Success() {
			err = &exec.ExitError{ProcessState: state}
		}
		waited <- err
	}()
	return spawned, waited, term, nil
}

func spawnInConsole(cmd *exec.Cmd, console windows.Handle, lifetime Lifetime, typed bool) (tree, error) {
	attributes, err := windows.NewProcThreadAttributeList(2)
	if err != nil {
		return tree{}, err
	}
	defer attributes.Delete()
	if err := attributes.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, *(*unsafe.Pointer)(unsafe.Pointer(&console)), unsafe.Sizeof(console)); err != nil {
		return tree{}, err
	}
	info := windows.StartupInfoEx{StartupInfo: windows.StartupInfo{Flags: windows.STARTF_USESTDHANDLES, StdInput: windows.InvalidHandle, StdOutput: windows.InvalidHandle, StdErr: windows.InvalidHandle},
		ProcThreadAttributeList: attributes.List()}
	if !typed {
		inheritable := windows.SecurityAttributes{InheritHandle: 1}
		inheritable.Length = uint32(unsafe.Sizeof(inheritable))
		nul, err := windows.CreateFile(windows.StringToUTF16Ptr("NUL"), windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, &inheritable, windows.OPEN_EXISTING, 0, 0)
		if err != nil {
			return tree{}, err
		}
		defer closeHandles(nul)
		if err := attributes.Update(windows.PROC_THREAD_ATTRIBUTE_HANDLE_LIST, unsafe.Pointer(&nul), unsafe.Sizeof(nul)); err != nil {
			return tree{}, err
		}
		info.StartupInfo = windows.StartupInfo{Flags: windows.STARTF_USESTDHANDLES, StdInput: nul}
	}
	info.Cb = uint32(unsafe.Sizeof(info))
	app, err := windows.UTF16PtrFromString(cmd.Path)
	if err != nil {
		return tree{}, err
	}
	line, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(cmd.Args))
	if err != nil {
		return tree{}, err
	}
	var dir *uint16
	if cmd.Dir != "" {
		if dir, err = windows.UTF16PtrFromString(cmd.Dir); err != nil {
			return tree{}, err
		}
	}
	env := utf16.Encode([]rune(strings.Join(cmd.Environ(), "\x00") + "\x00\x00"))
	var created windows.ProcessInformation
	flags := uint32(windows.CREATE_SUSPENDED | windows.CREATE_UNICODE_ENVIRONMENT | windows.EXTENDED_STARTUPINFO_PRESENT)
	if err := windows.CreateProcess(app, line, nil, nil, !typed, flags, &env[0], dir, &info.StartupInfo, &created); err != nil {
		return tree{}, err
	}
	defer closeHandles(created.Process, created.Thread)
	job, err := adoptIntoJob(int(created.ProcessId), lifetime)
	if err == nil {
		cmd.Process, err = os.FindProcess(int(created.ProcessId))
	}
	if err == nil {
		_, err = windows.ResumeThread(created.Thread)
	}
	if err != nil {
		_ = windows.TerminateProcess(created.Process, 1)
		closeHandles(job)
		cmd.Process = nil
		return tree{}, err
	}
	return tree{job: job}, nil
}
