package fs

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func openLocked(path string) (*os.File, bool, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, false, err
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE, windows.FILE_SHARE_READ, nil, windows.OPEN_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL, 0)
	switch {
	case errors.Is(err, windows.ERROR_SHARING_VIOLATION), errors.Is(err, windows.ERROR_ACCESS_DENIED):
		return nil, true, nil
	case err != nil:
		return nil, false, err
	}
	return os.NewFile(uintptr(handle), path), false, nil
}

func Unlock(file *os.File) error {
	closed := file.Close()
	_ = os.Remove(file.Name())
	return closed
}
