//go:build !windows

package host

import (
	"errors"
	"os"
	"syscall"
)

func lockHub(path string) (*os.File, bool, error) {
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
