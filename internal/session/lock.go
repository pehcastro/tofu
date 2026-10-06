package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

const (
	lockName                     = "lock"
	lockStaleGrace time.Duration = 3 * time.Second
	lockRetry      time.Duration = 50 * time.Millisecond
)

type BusyError struct {
	ID    string    `json:"session"`
	PID   int       `json:"pid"`
	Since time.Time `json:"since"`
}

func (e BusyError) Error() string {
	return fmt.Sprintf("session: %s is open for writing in process %d since %s, so it reads here and takes no second writer", e.ID, e.PID, e.Since.Format(time.TimeOnly))
}

type lockHolder struct {
	PID   int       `json:"pid"`
	Since time.Time `json:"since"`
}

func (s *Store) lock(id string) (*os.File, error) {
	path := filepath.Join(s.Dir(id), lockName)
	for deadline := time.Now().Add(lockStaleGrace); ; time.Sleep(lockRetry) {
		file, held, err := tryLock(path)
		if err != nil {
			return nil, err
		}
		if !held {
			body, _ := json.Marshal(lockHolder{PID: os.Getpid(), Since: time.Now()})
			err := file.Truncate(0)
			if err == nil {
				_, err = file.Write(body)
			}
			if err != nil {
				return nil, errors.Join(err, unlock(file))
			}
			return file, nil
		}
		holder := holderOf(path)
		switch {
		case holder.PID == os.Getpid():
			return nil, nil
		case holder.PID > 0 && alive(holder.PID), time.Now().After(deadline):
			return nil, BusyError{ID: id, PID: holder.PID, Since: holder.Since}
		}
	}
}

func (s *Store) Busy(id string) error {
	path := filepath.Join(s.Dir(id), lockName)
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	file, held, err := tryLock(path)
	switch {
	case err != nil:
		return err
	case !held:
		return file.Close()
	}
	if holder := holderOf(path); holder.PID != os.Getpid() {
		return BusyError{ID: id, PID: holder.PID, Since: holder.Since}
	}
	return nil
}

func holderOf(path string) lockHolder {
	var holder lockHolder
	if raw, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(raw, &holder)
	}
	return holder
}
