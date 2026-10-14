package tea

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/color"
	"io"
	"log"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/charmbracelet/colorprofile"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
	"github.com/muesli/cancelreader"
)

var ErrProgramPanic = errors.New("program experienced a panic")

var ErrProgramKilled = errors.New("program was killed")

var ErrInterrupted = errors.New("program was interrupted")

type Msg = uv.Event

type Model interface {
	Init() Cmd

	Update(Msg) (Model, Cmd)

	View() View
}

func NewView(s string) View {
	var view View
	view.SetContent(s)
	return view
}

type View struct {
	Content string

	OnMouse func(msg MouseMsg) Cmd

	Cursor *Cursor

	BackgroundColor color.Color

	ForegroundColor color.Color

	WindowTitle string

	ProgressBar *ProgressBar

	AltScreen bool

	ReportFocus bool

	DisableBracketedPasteMode bool

	MouseMode MouseMode

	KeyboardEnhancements KeyboardEnhancements
}

type KeyboardEnhancements struct {
	ReportEventTypes bool

	ReportAlternateKeys bool

	ReportAllKeysAsEscapeCodes bool

	ReportAssociatedText bool
}

func (v *View) SetContent(s string) {
	v.Content = s
}

type MouseMode int

const (
	MouseModeNone MouseMode = iota

	MouseModeCellMotion

	MouseModeAllMotion
)

type ProgressBarState int

const (
	ProgressBarNone ProgressBarState = iota
	ProgressBarDefault
	ProgressBarError
	ProgressBarIndeterminate
	ProgressBarWarning
)

func (s ProgressBarState) String() string {
	switch s {
	case ProgressBarNone:
		return "None"
	case ProgressBarDefault:
		return "Default"
	case ProgressBarError:
		return "Error"
	case ProgressBarIndeterminate:
		return "Indeterminate"
	case ProgressBarWarning:
		return "Warning"
	default:
		return "Unknown"
	}
}

type ProgressBar struct {
	State ProgressBarState

	Value int
}

func NewProgressBar(state ProgressBarState, value int) *ProgressBar {
	return &ProgressBar{
		State: state,
		Value: min(max(value, 0), 100),
	}
}

type Cursor struct {
	Position

	Color color.Color

	Shape CursorShape

	Blink bool
}

func NewCursor(x, y int) *Cursor {
	return &Cursor{
		Position: Position{X: x, Y: y},
		Color:    nil,
		Shape:    CursorBlock,
		Blink:    true,
	}
}

type Cmd func() Msg

type channelHandlers struct {
	handlers []chan struct{}
	mu       sync.RWMutex
}

func (h *channelHandlers) add(ch chan struct{}) {
	h.mu.Lock()
	h.handlers = append(h.handlers, ch)
	h.mu.Unlock()
}

func (h *channelHandlers) shutdown() {
	var wg sync.WaitGroup

	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, ch := range h.handlers {
		wg.Add(1)
		go func(ch chan struct{}) {
			<-ch
			wg.Done()
		}(ch)
	}
	wg.Wait()
}

type Program struct {
	disableInput bool

	disableSignalHandler bool

	disableCatchPanics bool

	filter func(Model, Msg) Msg

	fps int

	initialModel Model

	disableRenderer bool

	handlers channelHandlers

	ctx    context.Context
	cancel context.CancelFunc

	externalCtx context.Context

	msgs         chan Msg
	errs         chan error
	finished     chan struct{}
	shutdownOnce sync.Once

	profile *colorprofile.Profile

	output    io.Writer
	outputBuf bytes.Buffer

	ttyOutput           term.File
	previousOutputState *term.State
	renderer            renderer

	environ uv.Environ

	logger uv.Logger

	input io.Reader

	ttyInput              term.File
	previousTtyInputState *term.State
	cancelReader          cancelreader.CancelReader
	inputScanner          *uv.TerminalReader
	readLoopDone          chan struct{}

	ignoreSignals uint32

	once sync.Once

	rendererDone chan struct{}
	renderWake   chan struct{}

	width, height int

	useHardTabs bool

	useBackspace bool

	mu sync.Mutex
}

func Quit() Msg {
	return QuitMsg{}
}

type QuitMsg struct{}

func Suspend() Msg {
	return SuspendMsg{}
}

type SuspendMsg struct{}

type ResumeMsg struct{}

type InterruptMsg struct{}

func Interrupt() Msg {
	return InterruptMsg{}
}

func NewProgram(model Model, opts ...ProgramOption) *Program {
	p := &Program{
		initialModel: model,
		msgs:         make(chan Msg),
		errs:         make(chan error, 1),
		rendererDone: make(chan struct{}),
		renderWake:   make(chan struct{}, 1),
	}

	for _, opt := range opts {
		opt(p)
	}

	if p.externalCtx == nil {
		p.externalCtx = context.Background()
	}

	p.ctx, p.cancel = context.WithCancel(p.externalCtx)

	if p.output == nil {
		p.output = os.Stdout
	}

	if p.environ == nil {
		p.environ = os.Environ()
	}

	if p.fps < 1 {
		p.fps = defaultFPS
	} else if p.fps > maxFPS {
		p.fps = maxFPS
	}

	tracePath, traceOk := os.LookupEnv("TEA_TRACE")
	if traceOk && len(tracePath) > 0 {

		if f, err := os.OpenFile(tracePath, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o600); err == nil {
			p.logger = log.New(f, "bubbletea: ", log.LstdFlags|log.Lshortfile)
		}
	}

	return p
}

func (p *Program) handleSignals() chan struct{} {
	ch := make(chan struct{})

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		defer func() {
			signal.Stop(sig)
			close(ch)
		}()

		for {
			select {
			case <-p.ctx.Done():
				return

			case s := <-sig:
				if atomic.LoadUint32(&p.ignoreSignals) == 0 {
					switch s {
					case syscall.SIGINT:
						p.msgs <- InterruptMsg{}
					default:
						p.msgs <- QuitMsg{}
					}
					return
				}
			}
		}
	}()

	return ch
}

func (p *Program) handleResize() chan struct{} {
	ch := make(chan struct{})

	if p.ttyOutput != nil {

		go p.listenForResize(ch)
	} else {
		close(ch)
	}

	return ch
}

func (p *Program) handleCommands(cmds chan Cmd) chan struct{} {
	ch := make(chan struct{})

	go func() {
		defer close(ch)

		for {
			select {
			case <-p.ctx.Done():
				return

			case cmd := <-cmds:
				if cmd == nil {
					continue
				}

				go func() {

					if !p.disableCatchPanics {
						defer func() {
							if r := recover(); r != nil {
								p.recoverFromPanic(r)
							}
						}()
					}

					msg := cmd()
					p.Send(msg)
				}()
			}
		}
	}()

	return ch
}

func (p *Program) eventLoop(model Model, cmds chan Cmd) (Model, error) {
	for {
		select {
		case p.renderWake <- struct{}{}:
		default:
		}
		select {
		case <-p.ctx.Done():
			return model, nil

		case err := <-p.errs:
			return model, err

		case msg := <-p.msgs:
			msg = p.translateInputEvent(msg)

			if p.filter != nil {
				msg = p.filter(model, msg)
			}
			if msg == nil {
				continue
			}

			switch msg := msg.(type) {
			case QuitMsg:
				return model, nil

			case InterruptMsg:
				return model, ErrInterrupted

			case SuspendMsg:
				if suspendSupported {
					p.suspend()
				}

			case CapabilityMsg:
				switch msg.Content {
				case "RGB", "Tc":
					if *p.profile != colorprofile.TrueColor {
						tc := colorprofile.TrueColor
						p.profile = &tc
						go p.Send(ColorProfileMsg{*p.profile})
					}
				}

			case ModeReportMsg:
				switch msg.Mode {
				case ansi.ModeSynchronizedOutput:
					if msg.Value == ansi.ModeReset {

						p.renderer.setSyncdUpdates(true)
					}
				case ansi.ModeUnicodeCore:
					if msg.Value == ansi.ModeReset || msg.Value == ansi.ModeSet || msg.Value == ansi.ModePermanentlySet {
						p.renderer.setWidthMethod(ansi.GraphemeWidth)
					}
				}

			case MouseMsg:
				switch msg.(type) {
				case MouseClickMsg, MouseReleaseMsg, MouseWheelMsg, MouseMotionMsg:

					if cmd := p.renderer.onMouse(msg); cmd != nil {
						go p.Send(cmd())
					}
				}

			case readClipboardMsg:
				p.execute(ansi.RequestSystemClipboard)

			case setClipboardMsg:
				p.execute(ansi.SetSystemClipboard(string(msg)))

			case readPrimaryClipboardMsg:
				p.execute(ansi.RequestPrimaryClipboard)

			case setPrimaryClipboardMsg:
				p.execute(ansi.SetPrimaryClipboard(string(msg)))

			case backgroundColorMsg:
				p.execute(ansi.RequestBackgroundColor)

			case foregroundColorMsg:
				p.execute(ansi.RequestForegroundColor)

			case cursorColorMsg:
				p.execute(ansi.RequestCursorColor)

			case execMsg:

				p.exec(msg.cmd, msg.fn)

			case terminalVersion:
				p.execute(ansi.RequestNameVersion)

			case requestCapabilityMsg:
				p.execute(ansi.RequestTermcap(string(msg)))

			case BatchMsg:
				go p.execBatchMsg(msg)
				continue

			case sequenceMsg:
				go p.execSequenceMsg(msg)
				continue

			case WindowSizeMsg:
				p.renderer.resize(msg.Width, msg.Height)

			case windowSizeMsg:
				go p.checkResize()

			case requestCursorPosMsg:
				p.execute(ansi.RequestCursorPositionReport)

			case RawMsg:
				p.execute(fmt.Sprint(msg.Msg))

			case printLineMessage:
				p.renderer.insertAbove(msg.messageBody) //nolint:errcheck,gosec

			case clearScreenMsg:
				p.renderer.clearScreen()

			case ColorProfileMsg:
				p.renderer.setColorProfile(msg.Profile)
			}

			var cmd Cmd
			model, cmd = model.Update(msg)

			select {
			case <-p.ctx.Done():
				return model, nil
			case cmds <- cmd:
			}

			p.render(model)
		}
	}
}

func (p *Program) render(model Model) {
	if p.renderer != nil {
		p.renderer.render(model.View())
	}
}

func (p *Program) execSequenceMsg(msg sequenceMsg) {
	if !p.disableCatchPanics {
		defer func() {
			if r := recover(); r != nil {
				p.recoverFromGoPanic(r)
			}
		}()
	}

	for _, cmd := range msg {
		if cmd == nil {
			continue
		}
		msg := cmd()
		switch msg := msg.(type) {
		case BatchMsg:
			p.execBatchMsg(msg)
		case sequenceMsg:
			p.execSequenceMsg(msg)
		default:
			p.Send(msg)
		}
	}
}

func (p *Program) execBatchMsg(msg BatchMsg) {
	if !p.disableCatchPanics {
		defer func() {
			if r := recover(); r != nil {
				p.recoverFromGoPanic(r)
			}
		}()
	}

	var wg sync.WaitGroup
	for _, cmd := range msg {
		if cmd == nil {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()

			if !p.disableCatchPanics {
				defer func() {
					if r := recover(); r != nil {
						p.recoverFromGoPanic(r)
					}
				}()
			}

			msg := cmd()
			switch msg := msg.(type) {
			case BatchMsg:
				p.execBatchMsg(msg)
			case sequenceMsg:
				p.execSequenceMsg(msg)
			default:
				p.Send(msg)
			}
		}()
	}

	wg.Wait()
}

func shouldQuerySynchronizedOutput(environ uv.Environ) bool {
	termType := environ.Getenv("TERM")
	termProg, okTermProg := environ.LookupEnv("TERM_PROGRAM")
	_, okSSHTTY := environ.LookupEnv("SSH_TTY")
	_, okWTSession := environ.LookupEnv("WT_SESSION")

	return (!okTermProg && !okSSHTTY) ||
		okWTSession ||
		(okTermProg && !strings.Contains(termProg, "Apple") && !okSSHTTY) ||
		strings.Contains(termType, "ghostty") ||
		strings.Contains(termType, "wezterm") ||
		strings.Contains(termType, "alacritty") ||
		strings.Contains(termType, "kitty") ||
		strings.Contains(termType, "rio")
}

func (p *Program) Run() (returnModel Model, returnErr error) {
	if p.initialModel == nil {
		return nil, errors.New("bubbletea: InitialModel cannot be nil")
	}

	p.handlers = channelHandlers{}
	cmds := make(chan Cmd)

	p.finished = make(chan struct{})
	defer func() {
		close(p.finished)
	}()

	defer p.cancel()

	if p.disableInput {
		p.input = nil
	} else if p.input == nil {
		p.input = os.Stdin
		if !term.IsTerminal(os.Stdin.Fd()) {
			ttyIn, _, err := OpenTTY()
			if err != nil {
				return p.initialModel, fmt.Errorf("bubbletea: error opening TTY: %w", err)
			}
			p.input = ttyIn
		}
	}

	if !p.disableSignalHandler {
		p.handlers.add(p.handleSignals())
	}

	if !p.disableCatchPanics {
		defer func() {
			if r := recover(); r != nil {
				returnErr = fmt.Errorf("%w: %w", ErrProgramKilled, ErrProgramPanic)
				p.recoverFromPanic(r)
			}
		}()
	}

	if err := p.initTerminal(); err != nil {
		return p.initialModel, err
	}

	width, height := p.width, p.height
	if p.ttyOutput != nil {

		w, h, err := term.GetSize(p.ttyOutput.Fd())
		if err != nil {
			return p.initialModel, fmt.Errorf("bubbletea: error getting terminal size: %w", err)
		}

		width, height = w, h
	}

	p.width, p.height = width, height
	resizeMsg := WindowSizeMsg{Width: p.width, Height: p.height}

	if p.renderer == nil {
		if p.disableRenderer {
			p.renderer = &nilRenderer{}
		} else {

			r := newCursedRenderer(
				p.output,
				p.environ,
				p.width,
				p.height,
			)
			r.setLogger(p.logger)
			r.setNoInput(p.disableInput)

			mapNl := runtime.GOOS != "windows" && p.ttyInput == nil
			r.setOptimizations(p.useHardTabs, p.useBackspace, mapNl)
			p.renderer = r
		}
	}

	if p.profile == nil {
		cp := colorprofile.Detect(p.output, p.environ)
		p.profile = &cp
	}

	p.renderer.setColorProfile(*p.profile)
	go p.Send(ColorProfileMsg{*p.profile})

	go p.Send(resizeMsg)
	p.renderer.resize(resizeMsg.Width, resizeMsg.Height)

	go p.Send(EnvMsg(p.environ))

	model := p.initialModel
	if p.input != nil {
		if err := p.initInputReader(false); err != nil {
			return model, err
		}
	}

	p.startRenderer()

	if !p.disableRenderer && shouldQuerySynchronizedOutput(p.environ) {

		p.execute(ansi.RequestModeSynchronizedOutput +
			ansi.RequestModeUnicodeCore)
	}

	initCmd := model.Init()
	if initCmd != nil {
		ch := make(chan struct{})
		p.handlers.add(ch)

		go func() {
			defer close(ch)

			select {
			case cmds <- initCmd:
			case <-p.ctx.Done():
			}
		}()
	}

	p.render(model)

	p.handlers.add(p.handleResize())

	p.handlers.add(p.handleCommands(cmds))

	var err error
	model, err = p.eventLoop(model, cmds)

	if err == nil && len(p.errs) > 0 {
		err = <-p.errs
	}

	killed := p.externalCtx.Err() != nil || p.ctx.Err() != nil || err != nil
	if killed {
		if err == nil && p.externalCtx.Err() != nil {

			err = fmt.Errorf("%w: %w", ErrProgramKilled, p.externalCtx.Err())
		} else if err == nil && p.ctx.Err() != nil {

			err = ErrProgramKilled
		} else {

			err = fmt.Errorf("%w: %w", ErrProgramKilled, err)
		}
	} else {

		p.render(model)
	}

	p.shutdown(killed)

	return model, err
}

func (p *Program) Send(msg Msg) {
	select {
	case <-p.ctx.Done():
	case p.msgs <- msg:
	}
}

func (p *Program) Quit() {
	p.Send(Quit())
}

func (p *Program) Kill() {
	p.shutdown(true)
}

func (p *Program) Wait() {
	<-p.finished
}

func (p *Program) execute(seq string) {
	p.mu.Lock()
	_, _ = p.outputBuf.WriteString(seq)
	p.mu.Unlock()
}

func (p *Program) flush() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.outputBuf.Len() == 0 {
		return nil
	}
	if p.logger != nil {
		p.logger.Printf("output: %q", p.outputBuf.String())
	}
	_, err := p.output.Write(p.outputBuf.Bytes())
	p.outputBuf.Reset()
	if err != nil {
		return fmt.Errorf("error writing to output: %w", err)
	}
	return nil
}

func (p *Program) shutdown(kill bool) {
	p.shutdownOnce.Do(func() {
		p.cancel()

		p.handlers.shutdown()

		if p.cancelReader != nil {

			if p.cancelReader.Cancel() {
				if !kill {
					p.waitForReadLoop()
				}
			}
			_ = p.cancelReader.Close()
		}

		if p.renderer != nil {
			p.stopRenderer(kill)
		}

		_ = p.restoreTerminalState()
	})
}

func (p *Program) recoverFromPanic(r interface{}) {
	select {
	case p.errs <- ErrProgramPanic:
	default:
	}
	p.shutdown(true)

	rec := strings.ReplaceAll(fmt.Sprintf("%s", r), "\n", "\r\n")
	fmt.Fprintf(os.Stderr, "Caught panic:\r\n\r\n%s\r\n\r\nRestoring terminal...\r\n\r\n", rec)
	stack := strings.ReplaceAll(fmt.Sprintf("%s\n", debug.Stack()), "\n", "\r\n")
	fmt.Fprint(os.Stderr, stack)
	if v, err := strconv.ParseBool(os.Getenv("TEA_DEBUG")); err == nil && v {
		f, err := os.Create(fmt.Sprintf("bubbletea-panic-%d.log", time.Now().Unix()))
		if err == nil {
			defer f.Close()        //nolint:errcheck
			fmt.Fprintln(f, rec)   //nolint:errcheck
			fmt.Fprintln(f)        //nolint:errcheck
			fmt.Fprintln(f, stack) //nolint:errcheck
		}
	}
}

func (p *Program) recoverFromGoPanic(r interface{}) {
	select {
	case p.errs <- ErrProgramPanic:
	default:
	}
	p.cancel()

	rec := strings.ReplaceAll(fmt.Sprintf("%s", r), "\n", "\r\n")
	fmt.Fprintf(os.Stderr, "Caught panic:\r\n\r\n%s\r\n\r\nRestoring terminal...\r\n\r\n", rec)
	stack := strings.ReplaceAll(fmt.Sprintf("%s\n", debug.Stack()), "\n", "\r\n")
	fmt.Fprint(os.Stderr, stack)
	if v, err := strconv.ParseBool(os.Getenv("TEA_DEBUG")); err == nil && v {
		f, err := os.Create(fmt.Sprintf("bubbletea-panic-%d.log", time.Now().Unix()))
		if err == nil {
			defer f.Close()        //nolint:errcheck
			fmt.Fprintln(f, rec)   //nolint:errcheck
			fmt.Fprintln(f)        //nolint:errcheck
			fmt.Fprintln(f, stack) //nolint:errcheck
		}
	}
}

func (p *Program) ReleaseTerminal() error {
	return p.releaseTerminal(false)
}

func (p *Program) releaseTerminal(reset bool) error {
	atomic.StoreUint32(&p.ignoreSignals, 1)
	if p.cancelReader != nil {
		p.cancelReader.Cancel()
	}

	p.waitForReadLoop()

	if p.renderer != nil {
		p.stopRenderer(false)
		if reset {
			p.renderer.reset()
		}
	}

	return p.restoreTerminalState()
}

func (p *Program) RestoreTerminal() error {
	atomic.StoreUint32(&p.ignoreSignals, 0)

	if err := p.initTerminal(); err != nil {
		return err
	}
	if p.input != nil {
		if err := p.initInputReader(false); err != nil {
			return err
		}
	}

	p.startRenderer()

	go p.checkResize()

	return p.flush()
}

func (p *Program) Println(args ...any) {
	p.msgs <- printLineMessage{
		messageBody: fmt.Sprint(args...),
	}
}

func (p *Program) Printf(template string, args ...any) {
	p.msgs <- printLineMessage{
		messageBody: fmt.Sprintf(template, args...),
	}
}

func (p *Program) startRenderer() {
	framerate := time.Second / time.Duration(p.fps)

	p.once = sync.Once{}

	p.renderer.start()
	go func() {
		var flushed time.Time
		for {
			select {
			case <-p.rendererDone:
				return
			case <-p.renderWake:
			}
			if wait := time.Until(flushed.Add(framerate)); wait > 0 {
				select {
				case <-p.rendererDone:
					return
				case <-time.After(wait):
				}
			}
			flushed = time.Now()
			_ = p.flush()
			_ = p.renderer.flush(false)
		}
	}()
}

func (p *Program) stopRenderer(kill bool) {

	p.once.Do(func() {
		p.rendererDone <- struct{}{}
	})

	if !kill {

		_ = p.renderer.flush(true)
	}

	_ = p.renderer.close()
}
