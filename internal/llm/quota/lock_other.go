//go:build !windows

package quota

import (
	"errors"
	"os"
	"syscall"
)

func tryLock(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		return file, nil
	}
	_ = file.Close()
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return nil, nil
	}
	return nil, err
}
