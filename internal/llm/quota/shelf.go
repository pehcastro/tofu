package quota

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"tofu/internal/transport"
)

const (
	shelfDirName     = "latest"
	shelfKeyBytes    = 8
	shelfRenameTries = 20
)

type shelved struct {
	Report     Report         `json:"report"`
	FreshUntil time.Time      `json:"fresh_until"`
	HeardAt    time.Time      `json:"heard_at"`
	RetryAt    time.Time      `json:"retry_at"`
	Backoff    time.Duration  `json:"backoff"`
	Kind       transport.Kind `json:"kind"`
	Status     int            `json:"status"`
}

type shelf struct {
	data string
	lock string
}

func (p *Poller) shelf(account Account) shelf {
	sum := sha256.Sum256([]byte(account.AccountID))
	base := filepath.Join(p.dir, shelfDirName, string(account.Provider)+"-"+hex.EncodeToString(sum[:shelfKeyBytes]))
	return shelf{data: base + ".json", lock: base + ".lock"}
}

func (s shelf) read() shelved {
	var held shelved
	if raw, err := os.ReadFile(s.data); err == nil {
		_ = json.Unmarshal(raw, &held)
	}
	return held
}

func (s shelf) write(held shelved) error {
	raw, err := json.Marshal(held)
	if err != nil {
		return err
	}
	if err := os.WriteFile(s.data+".next", raw, 0o644); err != nil {
		return err
	}
	for range shelfRenameTries {
		if err = os.Rename(s.data+".next", s.data); err == nil {
			return nil
		}
		time.Sleep(lockRetry)
	}
	return err
}

func (s shelf) try() (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(s.lock), 0o755); err != nil {
		return nil, err
	}
	return tryLock(s.lock)
}

func (s shelf) take(ctx context.Context) (*os.File, error) {
	ctx, cancel := context.WithTimeout(ctx, pollTimeout)
	defer cancel()
	for {
		file, err := s.try()
		if file != nil || err != nil {
			return file, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(lockRetry):
		}
	}
}

func statusOf(err error) int {
	var failure *transport.Error
	if errors.As(err, &failure) {
		return failure.Status
	}
	return 0
}
