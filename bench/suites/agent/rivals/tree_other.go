//go:build !windows

package main

import (
	"os"
	"os/exec"
	"runtime"
	"syscall"
)

const treeMethod = "CPU is user plus system time of the child and every descendant it waited for, from rusage; peak tree RSS is the largest single maxrss, not a sum; peak commit and the process count are not measured"

type tree struct{}

func startTree(cmd *exec.Cmd) (*tree, error) {
	return &tree{}, cmd.Start()
}

func (*tree) finish(state *os.ProcessState) treeUsage {
	if state == nil {
		return treeUsage{}
	}
	maxrss := uint64(state.SysUsage().(*syscall.Rusage).Maxrss)
	if runtime.GOOS != "darwin" {
		maxrss *= 1024
	}
	return treeUsage{CPUMS: (state.UserTime() + state.SystemTime()).Milliseconds(), PeakTreeRSS: maxrss}
}
