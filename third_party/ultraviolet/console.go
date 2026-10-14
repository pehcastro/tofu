package uv

import (
	"io"
	"os"
	"runtime"

	"github.com/charmbracelet/x/term"
)

var isWindows = runtime.GOOS == "windows"

type File interface {
	io.ReadWriteCloser
	term.File

	Name() string
}

type Winsize struct {
	Row    uint16
	Col    uint16
	Xpixel uint16
	Ypixel uint16
}

type Console interface {
	io.ReadWriteCloser

	Environ() []string

	Getenv(key string) string

	LookupEnv(key string) (string, bool)

	Reader() io.Reader

	Writer() io.Writer

	MakeRaw() (state *term.State, err error)

	Restore() error

	GetSize() (width, height int, err error)

	GetWinsize() (*Winsize, error)
}

type TTY struct {
	*console
}

var _ Console = (*TTY)(nil)

type WinCon struct {
	*console
}

var _ Console = (*WinCon)(nil)

type console struct {
	input       File
	inputState  *term.State
	output      File
	outputState *term.State
	environ     Environ
}

func DefaultConsole() Console {
	return NewConsole(os.Stdin, os.Stdout, os.Environ())
}

func ControllingConsole() (Console, error) {
	inTty, outTty, err := OpenTTY()
	if err != nil {
		return nil, err
	}
	return NewConsole(inTty, outTty, os.Environ()), nil
}

func NewConsole(input, output File, environ []string) Console {
	if input == nil {
		input = os.Stdin
	}
	if output == nil {
		output = os.Stdout
	}
	if environ == nil {
		environ = os.Environ()
	}
	return newConsole(input, output, environ)
}

func (t *console) Environ() []string {
	return t.environ
}

func (t *console) Writer() io.Writer {
	return t.output
}

func (t *console) Reader() io.Reader {
	return t.input
}

func (t *console) Write(p []byte) (n int, err error) {
	return t.output.Write(p)
}

func (t *console) Read(p []byte) (n int, err error) {
	return t.input.Read(p)
}

func (t *console) Getenv(key string) string {
	return t.environ.Getenv(key)
}

func (t *console) LookupEnv(key string) (string, bool) {
	return t.environ.LookupEnv(key)
}

func (t *console) MakeRaw() (state *term.State, err error) {
	inState, outState, err := makeRaw(t.input, t.output)
	if err != nil {
		return nil, err
	}
	t.inputState = inState
	t.outputState = outState
	if inState != nil {
		return inState, nil
	}
	return outState, nil
}

func (t *console) Restore() error {
	if t.inputState != nil {
		if err := term.Restore(t.input.Fd(), t.inputState); err != nil {
			return err
		}
		t.inputState = nil
	}
	if t.outputState != nil {
		if err := term.Restore(t.output.Fd(), t.outputState); err != nil {
			return err
		}
		t.outputState = nil
	}
	return nil
}

func (t *console) Close() error {
	if err := t.Restore(); err != nil {
		return err
	}
	return nil
}

func (t *console) GetSize() (width, height int, err error) {
	return getSize(t.input, t.output)
}

func (t *console) GetWinsize() (*Winsize, error) {
	ws, err := getWinsize(t.input, t.output)
	if err != nil {
		return nil, err
	}
	return &ws, nil
}
