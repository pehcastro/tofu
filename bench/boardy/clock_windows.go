package main

import (
	"syscall"
	"time"
	"unsafe"
)

type clock struct {
	counter   *syscall.LazyProc
	perSecond int64
}

func newClock() clock {
	kernel := syscall.NewLazyDLL("kernel32.dll")
	c := clock{counter: kernel.NewProc("QueryPerformanceCounter")}
	_, _, _ = kernel.NewProc("QueryPerformanceFrequency").Call(uintptr(unsafe.Pointer(&c.perSecond)))
	return c
}

func (c clock) now() int64 {
	var ticks int64
	_, _, _ = c.counter.Call(uintptr(unsafe.Pointer(&ticks)))
	return ticks
}

func (c clock) since(start int64) time.Duration {
	return time.Duration((c.now() - start) * int64(time.Second) / c.perSecond)
}
