package retry

import (
	"errors"
	"testing"
)

func TestDoStopsAtFirstSuccess(t *testing.T) {
	calls := 0
	err := Do(4, func() error {
		calls++
		if calls < 2 {
			return errors.New("busy")
		}
		return nil
	})
	if err != nil || calls != 2 {
		t.Fatalf("err %v after %d calls", err, calls)
	}
}

func TestDoRefusesZero(t *testing.T) {
	if err := Do(0, func() error { return nil }); !errors.Is(err, ErrNoAttempts) {
		t.Fatalf("err %v", err)
	}
}
