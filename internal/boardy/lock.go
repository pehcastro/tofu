package boardy

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	locksDirName               = "locks"
	lockWait     time.Duration = 30 * time.Second
	lockRetry    time.Duration = 10 * time.Millisecond
)

func (s Store) locked(key, name string, work func() error) error {
	dir := filepath.Join(s.BoardDir(key), locksDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, name+".lock")
	for deadline := time.Now().Add(lockWait); ; time.Sleep(lockRetry) {
		file, held, err := tryLock(path)
		switch {
		case err != nil:
			return err
		case !held:
			return errors.Join(work(), unlock(file))
		case time.Now().After(deadline):
			return fmt.Errorf("%s stayed locked by another tofu for %s", path, lockWait)
		}
	}
}

func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tofu-*")
	if err != nil {
		return err
	}
	_, err = tmp.Write(data)
	if err = errors.Join(err, tmp.Close()); err != nil {
		return errors.Join(err, os.Remove(tmp.Name()))
	}
	for deadline := time.Now().Add(lockWait); ; time.Sleep(lockRetry) {
		err = os.Rename(tmp.Name(), path)
		if err == nil || time.Now().After(deadline) {
			break
		}
	}
	if err != nil {
		return errors.Join(err, os.Remove(tmp.Name()))
	}
	return nil
}
