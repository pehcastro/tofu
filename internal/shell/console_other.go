//go:build !windows && !linux

package shell

import (
	"errors"
	"io"
	"os/exec"
)

func startConsole(*exec.Cmd, io.Writer, Lifetime) (tree, <-chan error, error) {
	return tree{}, nil, errors.New("shell: no pseudo terminal is opened on this system")
}
