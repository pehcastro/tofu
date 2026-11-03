package shell

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"tofu/internal/konst"
)

type Holder string

const (
	HolderAgent  Holder = "agent"
	HolderPerson Holder = "person"
)

func (s Shell) HeldByPerson() bool { return s.Holder == HolderPerson }

type Refused string

const (
	RefusedUnknown      Refused = "unknown_shell"
	RefusedNotRunning   Refused = "not_running"
	RefusedNotKept      Refused = "not_kept"
	RefusedNoTerminal   Refused = "not_in_terminal"
	RefusedOtherProcess Refused = "other_process"
	RefusedNotHeld      Refused = "not_held"
)

func (r Refused) Error() string {
	switch r {
	case RefusedUnknown:
		return "no shell has that name in this project"
	case RefusedNotRunning:
		return "the shell has exited or been killed"
	case RefusedNotKept:
		return "the shell is still a foreground bash call, and only a kept shell is taken over"
	case RefusedNoTerminal:
		return "the shell was started on pipes, before a client declared takeover"
	case RefusedOtherProcess:
		return "the shell belongs to another tofu process"
	case RefusedNotHeld:
		return "the person does not hold the shell"
	}
	panic("shell: unknown refusal " + string(r))
}

type HeldError struct {
	Name  string
	Since time.Time
}

func (e HeldError) Error() string {
	return fmt.Sprintf("%s is held by the person, who took it over at %s. You cannot read, wait on, stop or restart it while they hold it. You will be told when they hand it back. Carry on with other work.",
		e.Name, e.Since.Local().Format(time.TimeOnly))
}

func (s Shell) refusedToAgent() error {
	if !s.HeldByPerson() {
		return nil
	}
	held := HeldError{Name: s.Name}
	if s.HeldSince != nil {
		held.Since = *s.HeldSince
	}
	return held
}

type Size struct{ cols, rows int }

func NewSize(cols, rows int) (Size, error) {
	if cols < konst.ShellMinColumns || cols > konst.ShellConsoleColumns || rows < 1 || rows > konst.ShellMaxRows {
		return Size{}, fmt.Errorf("cols is %d to %d and rows 1 to %d, so %d by %d is out of range", konst.ShellMinColumns, konst.ShellConsoleColumns, konst.ShellMaxRows, cols, rows)
	}
	return Size{cols: cols, rows: rows}, nil
}

type terminal struct {
	mu       sync.Mutex
	typing   sync.Mutex
	device   device
	screen   *screenText
	raw      []byte
	base     int64
	size     Size
	resized  bool
	answered chan struct{}
	ended    bool
	wrote    chan<- struct{}
}

func readerFor(out io.Writer, typed bool, d device) (io.Writer, *terminal) {
	screen := &screenText{out: out}
	if !typed {
		return screen, nil
	}
	term := &terminal{device: d, screen: screen}
	return term, term
}

func (t *terminal) Write(p []byte) (int, error) {
	t.mu.Lock()
	t.raw = append(t.raw, p...)
	if over := len(t.raw) - konst.ShellRawRingBytes; over > 0 {
		t.raw, t.base = t.raw[over:], t.base+int64(over)
	}
	t.screen.width = t.size.cols
	if t.resized && t.screen.repaint != repainting {
		t.screen.expectRepaint(t.size)
		t.resized = false
	}
	wrote := t.wrote
	t.mu.Unlock()
	n, err := t.screen.Write(p)
	if t.screen.printed {
		t.mu.Lock()
		if t.answered != nil {
			close(t.answered)
			t.answered = nil
		}
		t.mu.Unlock()
	}
	select {
	case wrote <- struct{}{}:
	default:
	}
	return n, err
}

func (t *terminal) tell(wrote chan<- struct{}) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.wrote = wrote
}

func (t *terminal) since(from int64) ([]byte, int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	from = min(max(from, t.base), t.base+int64(len(t.raw)))
	return slices.Clone(t.raw[from-t.base:]), from
}

func (t *terminal) resize(size Size) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.ended {
		return RefusedNotRunning
	}
	if err := t.device.resize(size); err != nil {
		return err
	}
	t.size, t.resized = size, true
	if resizeLosesKeys {
		t.answered = make(chan struct{})
	}
	return nil
}

func (t *terminal) awaitResizeAnswer() error {
	for try := 1; ; try++ {
		t.mu.Lock()
		answered, size := t.answered, t.size
		t.mu.Unlock()
		if answered == nil {
			return nil
		}
		select {
		case <-answered:
			return nil
		case <-time.After(konst.ShellResizeAckMillis * time.Millisecond):
		}
		if try == konst.ShellResizeTries {
			t.mu.Lock()
			t.answered = nil
			t.mu.Unlock()
			return nil
		}
		neighbour := Size{cols: size.cols, rows: size.rows + 1}
		if size.rows == konst.ShellMaxRows {
			neighbour.rows = size.rows - 1
		}
		if err := t.resize(neighbour); err != nil {
			return err
		}
		if err := t.resize(size); err != nil {
			return err
		}
	}
}

func (t *terminal) input(data []byte) error {
	t.typing.Lock()
	defer t.typing.Unlock()
	if err := t.awaitResizeAnswer(); err != nil {
		return err
	}
	t.mu.Lock()
	ended := t.ended
	t.mu.Unlock()
	if ended {
		return RefusedNotRunning
	}
	return t.device.write(data)
}

func (t *terminal) end() {
	t.typing.Lock()
	defer t.typing.Unlock()
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ended = true
}

type Hold struct {
	Shell  Shell
	Base   int64
	Next   int64
	Replay []byte
}

func (r *Registry) terminalOf(name string) (Shell, *terminal, error) {
	if !filepath.IsLocal(name) || filepath.Base(name) != name {
		return Shell{}, nil, RefusedUnknown
	}
	entry, err := r.readLocked(name)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return Shell{}, nil, RefusedUnknown
	case err != nil:
		return Shell{}, nil, err
	case entry.State != Running:
		return entry, nil, RefusedNotRunning
	case entry.TofuPID != r.self:
		return entry, nil, RefusedOtherProcess
	case entry.Kept == "":
		return entry, nil, RefusedNotKept
	}
	process := r.running[name]
	if process == nil || process.term == nil {
		return entry, nil, RefusedNoTerminal
	}
	return entry, process.term, nil
}

func (r *Registry) heldLocked(name string) (Shell, *terminal, error) {
	entry, term, err := r.terminalOf(name)
	var refused Refused
	switch {
	case errors.Is(err, RefusedUnknown), errors.Is(err, RefusedNotRunning):
		return entry, nil, err
	case errors.As(err, &refused), err == nil && !entry.HeldByPerson():
		return entry, nil, RefusedNotHeld
	}
	return entry, term, err
}

func (r *Registry) TakeOver(name string, size Size) (Hold, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, term, err := r.terminalOf(name)
	if err != nil {
		return Hold{}, err
	}
	if err := term.resize(size); err != nil {
		return Hold{}, err
	}
	term.tell(r.wrote)
	if !entry.HeldByPerson() {
		now := time.Now()
		entry.Holder, entry.HeldSince = HolderPerson, &now
		if err := r.writeLocked(entry); err != nil {
			return Hold{}, err
		}
	}
	replay, base := term.since(0)
	return Hold{Shell: entry, Base: base, Next: base + int64(len(replay)), Replay: replay}, nil
}

func (r *Registry) held(name string) (*terminal, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, term, err := r.heldLocked(name)
	return term, err
}

func (r *Registry) Input(name string, data []byte) error {
	term, err := r.held(name)
	if err != nil {
		return err
	}
	return term.input(data)
}

func (r *Registry) Resize(name string, size Size) error {
	term, err := r.held(name)
	if err != nil {
		return err
	}
	return term.resize(size)
}

func (r *Registry) HandBack(name string) (Shell, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, term, err := r.heldLocked(name)
	if err != nil {
		return entry, err
	}
	entry.Holder, entry.HeldSince = "", nil
	term.tell(nil)
	return entry, r.writeLocked(entry)
}

func (r *Registry) Wrote() <-chan struct{} { return r.wrote }

func (r *Registry) Raw(name string, from int64) ([]byte, int64) {
	r.mu.Lock()
	process := r.running[name]
	r.mu.Unlock()
	if process == nil || process.term == nil {
		return nil, from
	}
	return process.term.since(from)
}

func (r *Registry) AgentRead(name string) (Shell, error) {
	entry, err := r.Read(name)
	if err != nil {
		return entry, err
	}
	return entry, entry.refusedToAgent()
}
