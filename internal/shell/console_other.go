//go:build !windows && !linux

package shell

import (
	"errors"
	"io"
	"os/exec"
)

var errNoTerminal = errors.New("shell: no pseudo terminal is opened on this system")

type device struct{}

func (device) write([]byte) error { return errNoTerminal }

func (device) resize(Size) error { return errNoTerminal }

func startConsole(*exec.Cmd, io.Writer, Lifetime, bool) (tree, <-chan error, *terminal, error) {
	return tree{}, nil, nil, errNoTerminal
}
