//go:build linux

package shell

import (
	"io"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

func startConsole(cmd *exec.Cmd, out io.Writer, lifetime Lifetime) (tree, <-chan error, error) {
	leader, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		return tree{}, nil, err
	}
	follower, err := openFollower(leader)
	if err != nil {
		_ = leader.Close()
		return tree{}, nil, err
	}
	cmd.Stdout, cmd.Stderr = follower, follower
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 1}
	if lifetime == DiesWithTofu {
		endWithParent(cmd.SysProcAttr)
	}
	err = cmd.Start()
	_ = follower.Close()
	if err != nil {
		cmd.Stdout, cmd.Stderr, cmd.SysProcAttr = nil, nil, nil
		_ = leader.Close()
		return tree{}, nil, err
	}
	drained := make(chan struct{})
	go func() {
		_, _ = io.Copy(&screenText{out: out}, leader)
		close(drained)
	}()
	waited := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		select {
		case <-drained:
		case <-time.After(killWait):
		}
		_ = leader.Close()
		waited <- err
	}()
	return tree{}, waited, nil
}

func openFollower(leader *os.File) (*os.File, error) {
	if err := unix.IoctlSetPointerInt(int(leader.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		return nil, err
	}
	number, err := unix.IoctlGetInt(int(leader.Fd()), unix.TIOCGPTN)
	if err != nil {
		return nil, err
	}
	follower, err := os.OpenFile("/dev/pts/"+strconv.Itoa(number), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		return nil, err
	}
	fd := int(follower.Fd())
	state, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err == nil {
		state.Oflag &^= unix.OPOST
		err = unix.IoctlSetTermios(fd, unix.TCSETS, state)
	}
	if err == nil {
		err = unix.IoctlSetWinsize(fd, unix.TIOCSWINSZ, &unix.Winsize{Row: consoleRows, Col: consoleColumns})
	}
	if err != nil {
		_ = follower.Close()
		return nil, err
	}
	return follower, nil
}
