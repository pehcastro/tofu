//go:build !windows

package sys

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

func finalPath(path string) (string, error) {
	return filepath.EvalSymlinks(path)
}

func openLocked(path string) (*os.File, bool, error) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, false, err
	}
	err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		return file, false, nil
	}
	_ = file.Close()
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return nil, true, nil
	}
	return nil, false, err
}

func Unlock(file *os.File) error {
	return errors.Join(os.Remove(file.Name()), file.Close())
}
