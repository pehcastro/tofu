//go:build !windows

package mutate

import "os/exec"

type bound struct {
	cmd *exec.Cmd
}

func startBounded(cmd *exec.Cmd, _ uintptr) (*bound, error) {
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &bound{cmd: cmd}, nil
}

func (b *bound) kill() error {
	return b.cmd.Process.Kill()
}

func (b *bound) release() error {
	return nil
}
