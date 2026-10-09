package quota

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func tryLock(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	err = windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{})
	if err == nil {
		return file, nil
	}
	_ = file.Close()
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return nil, nil
	}
	return nil, err
}
