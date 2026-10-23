package host

import (
	"os"
	"path/filepath"
	"strconv"
)

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
