package session

import (
	"errors"

	"golang.org/x/sys/windows"
)

func alive(pid int) bool {
	process, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return errors.Is(err, windows.ERROR_ACCESS_DENIED)
	}
	defer func() { _ = windows.CloseHandle(process) }()
	state, err := windows.WaitForSingleObject(process, 0)
	return err == nil && state == uint32(windows.WAIT_TIMEOUT)
}
