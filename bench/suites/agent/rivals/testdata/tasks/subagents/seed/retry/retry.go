package retry

import "errors"

var ErrNoAttempts = errors.New("retry: attempts must be at least one")

func Do(attempts int, fn func() error) error {
	if attempts < 1 {
		return ErrNoAttempts
	}
	var err error
	for i := 0; i < attempts; i++ {
		if err = fn(); err == nil {
			return nil
		}
	}
	return err
}
