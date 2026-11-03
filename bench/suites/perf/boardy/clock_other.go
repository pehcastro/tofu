//go:build !windows

package main

import "time"

type clock struct {
	started time.Time
}

func newClock() clock {
	return clock{started: time.Now()}
}

func (c clock) now() int64 {
	return int64(time.Since(c.started))
}

func (c clock) since(start int64) time.Duration {
	return time.Duration(c.now() - start)
}
