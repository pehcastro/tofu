package session

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"tofu/internal/sys"
)

const (
	lockName                            = "lock"
	turnLockName                        = "turn.lock"
	channelName                         = "follow.sock"
	channelDirName                      = "sock"
	channelShortDir                     = "s"
	channelShortHashBytes               = 4
	channelPathLimit                    = 100
	channelHashBytes                    = 8
	lockStaleGrace        time.Duration = 3 * time.Second
	lockRetry             time.Duration = 50 * time.Millisecond
)

type BusyError struct {
	ID    string    `json:"session"`
	PID   int       `json:"pid"`
	Since time.Time `json:"since"`
}

func (e BusyError) Error() string {
	return fmt.Sprintf("session: %s is open for writing in process %d since %s, so it reads here and takes no second writer", e.ID, e.PID, e.Since.Format(time.TimeOnly))
}

func (e BusyError) Live() bool { return e.PID > 0 && alive(e.PID) }

type lockHolder struct {
	PID   int       `json:"pid"`
	Since time.Time `json:"since"`
}

type TurnBusyError struct {
	PID   int       `json:"pid"`
	Since time.Time `json:"since"`
}

func (e TurnBusyError) Error() string {
	return fmt.Sprintf("session: a session is already working in this project, in process %d since %s, and this project lets one session work at a time", e.PID, e.Since.Format(time.TimeOnly))
}

func (s *Store) lock(id string) (*os.File, error) {
	file, busy, err := takeLock(filepath.Join(s.Dir(id), lockName))
	switch {
	case busy == nil:
		return file, err
	case busy.PID == os.Getpid():
		return nil, nil
	}
	return nil, BusyError{ID: id, PID: busy.PID, Since: busy.Since}
}

func (s *Store) Hold(id string) (func() error, error) {
	if err := os.MkdirAll(s.Dir(id), 0o755); err != nil {
		return nil, err
	}
	file, err := s.lock(id)
	if file == nil {
		return nil, err
	}
	return func() error { return unlock(file) }, nil
}

func (s *Store) HoldTurn() (func() error, error) {
	if err := os.MkdirAll(s.State(), 0o755); err != nil {
		return nil, err
	}
	file, busy, err := takeLock(filepath.Join(s.State(), turnLockName))
	switch {
	case busy != nil:
		return nil, TurnBusyError{PID: busy.PID, Since: busy.Since}
	case err != nil:
		return nil, err
	}
	return func() error { return unlock(file) }, nil
}

func (s *Store) Channel(id string) string {
	beside := filepath.Join(s.Dir(id), channelName)
	home, err := sys.HomeConfigDir()
	if len(beside) < channelPathLimit || err != nil {
		return beside
	}
	sum := sha256.Sum256([]byte(id))
	if under := filepath.Join(home, channelDirName, "tofu-"+hex.EncodeToString(sum[:channelHashBytes])+".sock"); len(under) < channelPathLimit {
		return under
	}
	return filepath.Join(home, channelShortDir, hex.EncodeToString(sum[:channelShortHashBytes])+".sock")
}

func takeLock(path string) (*os.File, *lockHolder, error) {
	for deadline := time.Now().Add(lockStaleGrace); ; time.Sleep(lockRetry) {
		file, held, err := tryLock(path)
		if err != nil {
			return nil, nil, err
		}
		if !held {
			body, _ := json.Marshal(lockHolder{PID: os.Getpid(), Since: time.Now()})
			err := file.Truncate(0)
			if err == nil {
				_, err = file.Write(body)
			}
			if err != nil {
				return nil, nil, errors.Join(err, unlock(file))
			}
			return file, nil, nil
		}
		holder := holderOf(path)
		if holder.PID == os.Getpid() || holder.PID > 0 && alive(holder.PID) || time.Now().After(deadline) {
			return nil, &holder, nil
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
