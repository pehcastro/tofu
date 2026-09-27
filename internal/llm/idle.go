package llm

import (
	"errors"
	"io"
	"sync/atomic"
	"time"

	"tofu/internal/transport"
)

var ErrStreamIdle = errors.New("no byte of the stream arrived within the idle timeout")

type idleBody struct {
	body  io.ReadCloser
	idle  time.Duration
	timer *time.Timer
	quiet atomic.Bool
}

func WatchIdle(body io.ReadCloser, idle time.Duration) io.ReadCloser {
	watched := &idleBody{body: body, idle: idle}
	watched.timer = time.AfterFunc(idle, func() {
		watched.quiet.Store(true)
		_ = body.Close()
	})
	return watched
}

func (b *idleBody) Read(p []byte) (int, error) {
	n, err := b.body.Read(p)
	if b.quiet.Load() {
		return n, transport.Fail("llm.WatchIdle", transport.KindTimeout, ErrStreamIdle, "the stream was silent for %s", b.idle)
	}
	if n > 0 {
		b.timer.Reset(b.idle)
	}
	return n, err
}

func (b *idleBody) Close() error {
	b.timer.Stop()
	return b.body.Close()
}
