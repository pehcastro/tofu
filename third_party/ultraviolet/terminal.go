package uv

import (
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/charmbracelet/x/ansi"
	"golang.org/x/sync/errgroup"
)

const DefaultBufferSize = 4096

const DefaultEventTimeout = 100 * time.Millisecond

type Options struct {
	BufferSize int

	EventTimeout time.Duration

	LegacyKeyEncoding LegacyKeyEncoding

	LookupKeys bool

	UseTerminfoKeys bool

	Logger Logger
}

func DefaultOptions() *Options {
	return &Options{
		BufferSize:   DefaultBufferSize,
		EventTimeout: DefaultEventTimeout,
		LookupKeys:   true,
	}
}

type Terminal struct {
	con   Console
	opts  *Options
	scr   *TerminalScreen
	pr    pollReader
	buf   []byte
	evc   chan Event
	errg  errgroup.Group
	winch chan os.Signal
	donec chan struct{}
}

func DefaultTerminal() *Terminal {
	return NewTerminal(nil, nil)
}

func ControllingTerminal() (*Terminal, error) {
	con, err := ControllingConsole()
	if err != nil {
		return nil, err
	}
	return NewTerminal(con, nil), nil
}

func NewTerminal(con Console, opts *Options) *Terminal {
	t := &Terminal{}
	if con == nil {
		con = DefaultConsole()
	}
	if opts == nil {
		opts = DefaultOptions()
	}
	if opts.BufferSize <= 0 {
		opts.BufferSize = DefaultBufferSize
	}
	if opts.EventTimeout <= 0 {
		opts.EventTimeout = DefaultEventTimeout
	}
	t.con = con
	t.opts = opts
	t.scr = NewTerminalScreen(t.con.Writer(), t.con.Environ())
	t.buf = make([]byte, opts.BufferSize)

	t.evc = make(chan Event)
	t.winch = make(chan os.Signal, 1)
	if opts.Logger != nil {
		t.scr.rend.SetLogger(opts.Logger)
	}
	return t
}

func (t *Terminal) GetSize() (width, height int, err error) {
	w, h, err := t.con.GetSize()
	if err != nil {
		return 0, 0, fmt.Errorf("getting terminal size: %w", err)
	}
	return int(w), int(h), nil
}

func (t *Terminal) GetWinsize() (*Winsize, error) {
	ws, err := t.con.GetWinsize()
	if err != nil {
		return nil, fmt.Errorf("getting terminal winsize: %w", err)
	}
	return ws, nil
}

func (t *Terminal) Screen() *TerminalScreen {
	return t.scr
}

func (t *Terminal) Events() <-chan Event {
	return t.evc
}

func (t *Terminal) Start() error {
	_, err := t.con.MakeRaw()
	if err != nil {
		return fmt.Errorf("failed to set terminal to raw mode: %w", err)
	}

	evs := newEventScanner()
	evs.lookup = t.opts.LookupKeys
	if evs.lookup {
		evs.table = buildKeysTable(t.opts.LegacyKeyEncoding, t.con.Getenv("TERM"), t.opts.UseTerminfoKeys)
	}
	if t.opts.Logger != nil {
		evs.setLogger(t.opts.Logger)
	}
	bufc := make(chan []byte)
	t.donec = make(chan struct{})
	t.pr, err = newPollReader(t.con.Reader())
	if err != nil {
		return fmt.Errorf("failed to create poll reader: %w", err)
	}

	t.errg.Go(func() error {
		for {
			n, err := t.pr.Read(t.buf)
			if err != nil {
				return fmt.Errorf("reading terminal input: %w", err)
			}
			select {
			case bufc <- t.buf[:n]:
			case <-t.donec:
				return nil
			}
		}
	})

	sendEvents := func(buf []byte, expired bool) int {
		n, events := evs.scanEvents(buf, expired)
		for _, ev := range events {
			t.handleEvent(ev)
			t.SendEvent(ev)
		}
		return n
	}
	t.errg.Go(func() error {
		var buf []byte
		timer := time.NewTimer(t.opts.EventTimeout)
		timeout := time.Now().Add(t.opts.EventTimeout)

		for {
			select {
			case <-t.donec:
				return nil
			case <-timer.C:
				expired := len(buf) > 0 && time.Now().After(timeout)
				n := sendEvents(buf, expired)
				if n > 0 {
					buf = buf[min(n, len(buf)):]
				}
				if len(buf) > 0 {
					timer.Reset(t.opts.EventTimeout)
				}
			case data := <-bufc:
				buf = append(buf, data...)
				n := sendEvents(buf, false)
				timeout = time.Now().Add(t.opts.EventTimeout)
				timer.Stop()
				if n > 0 {
					buf = buf[min(n, len(buf)):]
				}
				if len(buf) > 0 {
					timer.Reset(t.opts.EventTimeout)
				}
			}
		}
	})

	sendWinsize := func() error {
		ws, err := t.con.GetWinsize()
		if err != nil {
			return fmt.Errorf("getting terminal size: %w", err)
		}
		if ws.Col > 0 && ws.Row > 0 {
			t.SendEvent(WindowSizeEvent{
				Width:  int(ws.Col),
				Height: int(ws.Row),
			})
		}
		if ws.Xpixel > 0 && ws.Ypixel > 0 {
			t.SendEvent(PixelSizeEvent{
				Width:  int(ws.Xpixel),
				Height: int(ws.Ypixel),
			})
		}
		return nil
	}

	NotifyWinch(t.winch)
	t.errg.Go(func() error {
		for {
			select {
			case <-t.donec:
				return nil
			case <-t.winch:
				if err := sendWinsize(); err != nil {
					return err
				}
			}
		}
	})

	t.errg.Go(func() error {
		if err := sendWinsize(); err != nil {
			return err
		}
		return nil
	})

	t.scr.Restore()

	t.scr.requestGraphemeWidth()
	if err := t.scr.Flush(); err != nil {
		return fmt.Errorf("failed to flush terminal screen: %w", err)
	}

	return nil
}

func (t *Terminal) handleEvent(ev Event) {
	if mr, ok := ev.(ModeReportEvent); ok {
		if mr.Mode == ansi.ModeUnicodeCore &&
			(mr.Value.IsSet() || mr.Value == ansi.ModeReset) {
			t.scr.enableGraphemeWidth()
		}
	}
}

func (t *Terminal) Wait() error {
	if err := t.errg.Wait(); err != nil {
		return fmt.Errorf("terminal event loop error: %w", err)
	}
	return nil
}

func (t *Terminal) Stop() error {
	if t.donec != nil {
		select {
		case <-t.donec:

		default:
			close(t.donec)
		}
	}
	if t.winch != nil {
		signal.Stop(t.winch)
	}
	if t.pr != nil {
		t.pr.Cancel()
		_ = t.pr.Close()
		t.pr = nil
	}
	t.scr.Reset()
	if err := t.scr.Flush(); err != nil {
		_ = t.con.Restore()
		return fmt.Errorf("failed to flush terminal screen: %w", err)
	}
	if err := t.con.Restore(); err != nil {
		return fmt.Errorf("failed to restore terminal state: %w", err)
	}
	return nil
}

func (t *Terminal) SendEvent(e Event) {
	select {
	case t.evc <- e:
	case <-t.donec:
	}
}

func (t *Terminal) Write(p []byte) (n int, err error) {
	return t.con.Write(p)
}

func (t *Terminal) Read(p []byte) (n int, err error) {
	return t.con.Read(p)
}
