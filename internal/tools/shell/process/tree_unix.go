//go:build !windows

package process

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"tofu/internal/platform/outside"
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

func processesNamed(name string) []int {
	listed, err := outside.Command(context.Background(), "pgrep", "-x", processName(name)).Output()
	if err != nil {
		return nil
	}
	var pids []int
	for _, field := range strings.Fields(string(listed)) {
		if pid, err := strconv.Atoi(field); err == nil {
			pids = append(pids, pid)
		}
	}
	return pids
}

func treeCPU(pid int) (time.Duration, error) {
	listed, err := outside.Command(context.Background(), "ps", "-A", "-o", "pgid=,time=").Output()
	if err != nil {
		return 0, err
	}
	var used time.Duration
	for _, row := range strings.Split(string(listed), "\n") {
		if fields := strings.Fields(row); len(fields) == 2 && fields[0] == strconv.Itoa(pid) {
			used += psTime(fields[1])
		}
	}
	return used, nil
}

func psTime(column string) time.Duration {
	var whole int
	if days, clock, hasDays := strings.Cut(column, "-"); hasDays {
		whole, _ = strconv.Atoi(days)
		column = clock
	}
	var seconds float64
	for _, part := range strings.Split(column, ":") {
		n, _ := strconv.ParseFloat(part, 64)
		seconds = seconds*60 + n
	}
	return time.Duration(whole)*24*time.Hour + time.Duration(seconds*float64(time.Second))
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
