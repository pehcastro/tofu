//go:build !windows

package memtree

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

func takeLock(path string) (*os.File, error) {
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
		return nil, fmt.Errorf("%s is held by another writer, and a log takes one", path)
	}
	return nil, err
}
