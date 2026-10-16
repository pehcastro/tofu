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

	"tofu/internal/konst"
	"tofu/internal/safe"
)

type device struct{ leader *os.File }

func (d device) write(data []byte) error {
	_, err := d.leader.Write(data)
	return err
}

func (d device) resize(size Size) error {
	return unix.IoctlSetWinsize(int(d.leader.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: uint16(size.rows), Col: uint16(size.cols)})
}

func startConsole(cmd *exec.Cmd, out io.Writer, lifetime Lifetime, typed bool, report safe.Report) (tree, <-chan error, *terminal, error) {
	leader, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		return tree{}, nil, nil, err
	}
	follower, err := openFollower(leader, typed)
	if err != nil {
		_ = leader.Close()
		return tree{}, nil, nil, err
	}
	cmd.Stdout, cmd.Stderr = follower, follower
	if typed {
		cmd.Stdin = follower
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 1}
	if lifetime == DiesWithTofu {
		endWithParent(cmd.SysProcAttr)
	}
	err = cmd.Start()
	_ = follower.Close()
	if err != nil {
		cmd.Stdin, cmd.Stdout, cmd.Stderr, cmd.SysProcAttr = nil, nil, nil, nil
		_ = leader.Close()
		return tree{}, nil, nil, err
	}
	sink, term := readerFor(out, typed, device{leader: leader})
	drained := make(chan struct{})
	safe.Go(report, "shell output", func() {
		defer close(drained)
		pipeOut(report, sink, leader)
	})
	waited := make(chan error, 1)
	safe.Go(report, "shell wait", func() {
		waited <- safe.Call(report, "shell wait", func() error {
			defer func() { _ = leader.Close() }()
			err := cmd.Wait()
			if term != nil {
				term.end()
			}
			select {
			case <-drained:
			case <-time.After(killWait):
			}
			return err
		})
	})
	return tree{}, waited, term, nil
}

func openFollower(leader *os.File, typed bool) (*os.File, error) {
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
	if err == nil && !typed {
		state.Oflag &^= unix.OPOST
		err = unix.IoctlSetTermios(fd, unix.TCSETS, state)
	}
	if err == nil {
		err = unix.IoctlSetWinsize(fd, unix.TIOCSWINSZ, &unix.Winsize{Row: konst.ShellConsoleRows, Col: konst.ShellConsoleColumns})
	}
	if err != nil {
		_ = follower.Close()
		return nil, err
	}
	return follower, nil
}
