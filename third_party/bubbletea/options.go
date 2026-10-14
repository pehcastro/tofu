package tea

import (
	"context"
	"io"
	"sync/atomic"

	"github.com/charmbracelet/colorprofile"
)

type ProgramOption func(*Program)

func WithContext(ctx context.Context) ProgramOption {
	return func(p *Program) {
		p.externalCtx = ctx
	}
}

func WithOutput(output io.Writer) ProgramOption {
	return func(p *Program) {
		p.output = output
	}
}

func WithInput(input io.Reader) ProgramOption {
	return func(p *Program) {
		p.input = input
		p.disableInput = input == nil
	}
}

func WithEnvironment(env []string) ProgramOption {
	return func(p *Program) {
		p.environ = env
	}
}

func WithoutSignalHandler() ProgramOption {
	return func(p *Program) {
		p.disableSignalHandler = true
	}
}

func WithoutCatchPanics() ProgramOption {
	return func(p *Program) {
		p.disableCatchPanics = true
	}
}

func WithoutSignals() ProgramOption {
	return func(p *Program) {
		atomic.StoreUint32(&p.ignoreSignals, 1)
	}
}

func WithoutRenderer() ProgramOption {
	return func(p *Program) {
		p.disableRenderer = true
	}
}

func WithFilter(filter func(Model, Msg) Msg) ProgramOption {
	return func(p *Program) {
		p.filter = filter
	}
}

func WithFPS(fps int) ProgramOption {
	return func(p *Program) {
		p.fps = fps
	}
}

func WithColorProfile(profile colorprofile.Profile) ProgramOption {
	return func(p *Program) {
		p.profile = &profile
	}
}

func WithWindowSize(width, height int) ProgramOption {
	return func(p *Program) {
		p.width = width
		p.height = height
	}
}
