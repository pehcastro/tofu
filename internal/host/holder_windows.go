//go:build windows

package host

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

const holderFlags = syscall.CREATE_NEW_PROCESS_GROUP | windows.CREATE_NO_WINDOW

func startDetached(command func() *exec.Cmd) error {
	outsideTheJob := command()
	outsideTheJob.SysProcAttr = &syscall.SysProcAttr{CreationFlags: holderFlags | windows.CREATE_BREAKAWAY_FROM_JOB}
	if outsideTheJob.Start() == nil {
		return outsideTheJob.Process.Release()
	}
	insideTheJob := command()
	insideTheJob.SysProcAttr = &syscall.SysProcAttr{CreationFlags: holderFlags}
	if err := insideTheJob.Start(); err != nil {
		return err
	}
	return insideTheJob.Process.Release()
}
