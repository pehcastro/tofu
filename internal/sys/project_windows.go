package sys

import (
	"errors"
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

const finalPathAsDOSDrive = 0

func finalPath(path string) (string, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	handle, err := windows.CreateFile(name, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return "", err
	}
	defer func() { _ = windows.CloseHandle(handle) }()
	buffer := make([]uint16, windows.MAX_PATH)
	for {
		length, err := windows.GetFinalPathNameByHandle(handle, &buffer[0], uint32(len(buffer)), finalPathAsDOSDrive)
		if err != nil {
			return "", err
		}
		if int(length) < len(buffer) {
			final := windows.UTF16ToString(buffer[:length])
			if unc, found := strings.CutPrefix(final, `\\?\UNC\`); found {
				return `\\` + unc, nil
			}
			return strings.TrimPrefix(final, `\\?\`), nil
		}
		buffer = make([]uint16, length)
	}
}

func tryLock(path string) (*os.File, bool, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, false, err
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE, windows.FILE_SHARE_READ, nil, windows.OPEN_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL, 0)
	switch {
	case errors.Is(err, windows.ERROR_SHARING_VIOLATION):
		return nil, true, nil
	case err != nil:
		return nil, false, err
	}
	return os.NewFile(uintptr(handle), path), false, nil
}

func unlock(file *os.File) error {
	closed := file.Close()
	_ = os.Remove(file.Name())
	return closed
}
