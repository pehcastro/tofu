//go:build !windows && !linux

package process

import (
	"errors"
	"io"
	"os/exec"

	"tofu/internal/platform/recover"
)

var errNoTerminal = errors.New("shell: no pseudo terminal is opened on this system")

const resizeLosesKeys = false

type device struct{}

func (device) write([]byte) error { return errNoTerminal }

func (device) resize(Size) error { return errNoTerminal }

func startConsole(*exec.Cmd, io.Writer, Lifetime, bool, recover.Report) (tree, <-chan error, *terminal, error) {
	return tree{}, nil, nil, errNoTerminal
}
