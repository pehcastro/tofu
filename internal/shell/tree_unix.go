//go:build !windows

package shell

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

type tree struct{}

func startTree(cmd *exec.Cmd, lifetime Lifetime) (tree, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if lifetime == DiesWithTofu {
		endWithParent(cmd.SysProcAttr)
	}
	if cmd.Cancel != nil {
		cmd.Cancel = func() error {
			if err := killTree(cmd.Process.Pid); !errors.Is(err, ErrTreeGone) {
				return err
			}
			return os.ErrProcessDone
		}
	}
	return tree{}, cmd.Start()
}

func (t tree) release() {}

func treeAlive(pid int) bool { return syscall.Kill(-pid, 0) == nil }

func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func treeHas(root, pid int) bool {
	group, err := syscall.Getpgid(pid)
	return err == nil && group == root
}

func killTree(pid int) error {
	err := syscall.Kill(-pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return ErrTreeGone
	}
	return err
}
