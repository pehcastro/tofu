//go:build !windows

package shell

import (
	"errors"
	"os/exec"
	"syscall"
)

type tree struct{}

func startTree(cmd *exec.Cmd, _ Lifetime) (tree, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return tree{}, cmd.Start()
}

func (t tree) release() {}

func treeAlive(pid int) bool { return syscall.Kill(-pid, 0) == nil }

func killTree(pid int) error {
	err := syscall.Kill(-pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return ErrTreeGone
	}
	return err
}
