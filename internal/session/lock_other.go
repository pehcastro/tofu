//go:build !windows

package session

import (
	"errors"
	"os"
	"syscall"
)

func tryLock(path string) (*os.File, bool, error) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, false, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, true, nil
		}
		return nil, false, err
	}
	opened, err := file.Stat()
	named, namedErr := os.Stat(path)
	if err != nil || namedErr != nil || !os.SameFile(opened, named) {
		_ = file.Close()
		return nil, true, nil
	}
	return file, false, nil
}

func unlock(file *os.File) error {
	return errors.Join(os.Remove(file.Name()), file.Close())
}

func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }
