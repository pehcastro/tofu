package tea

import (
	"io"
	"os"
	"os/exec"
)

type execMsg struct {
	cmd ExecCommand
	fn  ExecCallback
}

func Exec(c ExecCommand, fn ExecCallback) Cmd {
	return func() Msg {
		return execMsg{cmd: c, fn: fn}
	}
}

func ExecProcess(c *exec.Cmd, fn ExecCallback) Cmd {
	return Exec(wrapExecCommand(c), fn)
}

type ExecCallback func(error) Msg

type ExecCommand interface {
	Run() error
	SetStdin(io.Reader)
	SetStdout(io.Writer)
	SetStderr(io.Writer)
}

func wrapExecCommand(c *exec.Cmd) ExecCommand {
	return &osExecCommand{Cmd: c}
}

type osExecCommand struct{ *exec.Cmd }

func (c *osExecCommand) SetStdin(r io.Reader) {

	if c.Stdin == nil {
		c.Stdin = r
	}
}

func (c *osExecCommand) SetStdout(w io.Writer) {

	if c.Stdout == nil {
		c.Stdout = w
	}
}

func (c *osExecCommand) SetStderr(w io.Writer) {

	if c.Stderr == nil {
		c.Stderr = w
	}
}

func (p *Program) exec(c ExecCommand, fn ExecCallback) {
	if err := p.releaseTerminal(false); err != nil {

		if fn != nil {
			go p.Send(fn(err))
		}
		return
	}

	c.SetStdin(p.input)
	c.SetStdout(p.output)
	c.SetStderr(os.Stderr)

	if err := c.Run(); err != nil {
		_ = p.RestoreTerminal()
		if fn != nil {
			go p.Send(fn(err))
		}
		return
	}

	err := p.RestoreTerminal()
	if fn != nil {
		go p.Send(fn(err))
	}
}
