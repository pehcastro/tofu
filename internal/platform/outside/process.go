package outside

import (
	"context"
	"os/exec"
)

type Cmd struct {
	*exec.Cmd
	ctx     context.Context
	waiting func()
}

func Command(ctx context.Context, name string, args ...string) *Cmd {
	return &Cmd{Cmd: exec.CommandContext(ctx, name, args...), ctx: ctx}
}

func (c *Cmd) StartWith(start func(*exec.Cmd) error) error {
	c.waiting = Wait(c.ctx, Process)
	err := start(c.Cmd)
	if err != nil {
		c.waiting()
	}
	return err
}

func (c *Cmd) Wait() error {
	if c.waiting == nil {
		c.waiting = Wait(c.ctx, Process)
	}
	defer c.waiting()
	return c.Cmd.Wait()
}

func (c *Cmd) Run() error {
	defer Wait(c.ctx, Process)()
	return c.Cmd.Run()
}

func (c *Cmd) Output() ([]byte, error) {
	defer Wait(c.ctx, Process)()
	return c.Cmd.Output()
}

func (c *Cmd) CombinedOutput() ([]byte, error) {
	defer Wait(c.ctx, Process)()
	return c.Cmd.CombinedOutput()
}
