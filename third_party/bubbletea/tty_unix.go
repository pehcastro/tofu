//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris || aix || zos

package tea

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/charmbracelet/x/term"
)

func (p *Program) initInput() (err error) {

	if f, ok := p.input.(term.File); ok && term.IsTerminal(f.Fd()) {
		p.ttyInput = f
		p.previousTtyInputState, err = term.MakeRaw(p.ttyInput.Fd())
		if err != nil {
			return fmt.Errorf("error entering raw mode: %w", err)
		}

		p.checkOptimizedMovements(p.previousTtyInputState)
	}

	if f, ok := p.output.(term.File); ok && term.IsTerminal(f.Fd()) {
		p.ttyOutput = f
	}

	return nil
}

const suspendSupported = true

func suspendProcess() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGCONT)
	defer signal.Stop(c)
	_ = syscall.Kill(0, syscall.SIGTSTP)

	<-c
}
