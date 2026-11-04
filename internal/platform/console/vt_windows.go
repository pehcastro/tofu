//go:build windows

package console

import "golang.org/x/sys/windows"

func EnableVT(fd uintptr) error {
	var mode uint32
	if err := windows.GetConsoleMode(windows.Handle(fd), &mode); err != nil {
		return err
	}
	return windows.SetConsoleMode(windows.Handle(fd), mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING)
}
