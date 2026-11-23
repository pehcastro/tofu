package host

import (
	"os"

	"golang.org/x/sys/unix"
)

func hubSocketOf(home, name string) (string, error) {
	return tempSocketOf(home, name)
}

func throughSocketDir(socket string, use func(string) error) error {
	return use(socket)
}

func errorOutputInto(file *os.File) (restore func(), err error) {
	was, err := unix.Dup(int(os.Stderr.Fd()))
	if err != nil {
		return nil, err
	}
	if err := unix.Dup2(int(file.Fd()), int(os.Stderr.Fd())); err != nil {
		_ = unix.Close(was)
		return nil, err
	}
	return func() {
		_ = unix.Dup2(was, int(os.Stderr.Fd()))
		_ = unix.Close(was)
	}, nil
}
