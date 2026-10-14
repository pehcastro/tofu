package uv

import (
	"fmt"
	"time"
)

type pollReader interface {
	Read(p []byte) (n int, err error)

	Poll(timeout time.Duration) (ready bool, err error)

	Cancel() bool

	Close() error
}

var ErrCanceled = fmt.Errorf("poll canceled")
