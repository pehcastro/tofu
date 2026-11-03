package main

import (
	"os"
	"time"

	"golang.org/x/sys/unix"
)

func processStarted() (time.Time, bool) {
	process, err := unix.SysctlKinfoProc("kern.proc.pid", os.Getpid())
	if err != nil {
		return time.Time{}, false
	}
	return time.Unix(process.Proc.P_starttime.Unix()), true
}
