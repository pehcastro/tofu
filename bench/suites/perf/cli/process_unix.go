//go:build !windows

package main

import (
	"os"
	"runtime"
	"syscall"
)

type meter struct{}

const cpuMethod = "CPU is user plus system time from rusage; RSS is maxrss"

func newMeter() meter {
	return meter{}
}

func (meter) watch(int) (func(*os.ProcessState) usage, error) {
	return func(state *os.ProcessState) usage {
		maxrss := uint64(state.SysUsage().(*syscall.Rusage).Maxrss)
		if runtime.GOOS != "darwin" {
			maxrss *= 1024
		}
		return usage{cpu: state.UserTime() + state.SystemTime(), peakRSS: maxrss}
	}, nil
}
