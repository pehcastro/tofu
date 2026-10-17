//go:build !windows

package host

import (
	"os/exec"
	"syscall"
)

func startDetached(command func() *exec.Cmd) error {
	cmd := command()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
