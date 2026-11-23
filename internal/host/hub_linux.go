package host

import (
	"os"
	"path/filepath"
	"strconv"

	"golang.org/x/sys/unix"
)

func errorOutputInto(file *os.File) (restore func(), err error) {
	was, err := unix.Dup(int(os.Stderr.Fd()))
	if err != nil {
		return nil, err
	}
	if err := unix.Dup3(int(file.Fd()), int(os.Stderr.Fd()), 0); err != nil {
		_ = unix.Close(was)
		return nil, err
	}
	return func() {
		_ = unix.Dup3(was, int(os.Stderr.Fd()), 0)
		_ = unix.Close(was)
	}, nil
}

const procSelfFD = "/proc/self/fd/"

func hubSocketOf(home, name string) (string, error) {
	return filepath.Join(home, hubSocketDir, name+".sock"), nil
}

func throughSocketDir(socket string, use func(string) error) error {
	dir, err := os.Open(filepath.Dir(socket))
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	return use(procSelfFD + strconv.Itoa(int(dir.Fd())) + "/" + filepath.Base(socket))
}
