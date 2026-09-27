package tui

import (
	"context"
	"io"
	"os"
	"os/exec"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/edits"
	"tofu/interface/tui/feed"
	"tofu/interface/tui/frame"
	"tofu/interface/tui/markdown"
	"tofu/interface/tui/paste"
	"tofu/interface/tui/pointer"
	"tofu/interface/tui/session"
	"tofu/interface/tui/settings"
	"tofu/interface/tui/shells"
	"tofu/interface/tui/subagent"
	"tofu/internal/judge/jev"
	"tofu/internal/keymap"
	"tofu/internal/konst"
	"tofu/internal/llm"
	library "tofu/internal/llm/models"
	isession "tofu/internal/session"
	isettings "tofu/internal/settings"
	isubagent "tofu/internal/subagent"
	"tofu/internal/sys"
)

type EventKind int

const (
	EventText EventKind = iota
	EventTextDelta
	EventToolCall
	EventToolResult
	EventNote
	EventFailure
	EventStats
	EventDone
	EventDecision
	EventGateOff
	EventContext
	EventForkStart
	EventForkEnd
	EventSubAgent
	EventAwaitPerson
	EventResumed
	EventSteered
	EventRequesting
	EventPlan
	EventSession
	EventTask
	EventStreamReset
	EventThinking
)

type Event struct {
	Kind      EventKind
	ID        string
	Tool      string
	Text      string
	Detail    string
	Bytes     int
	Failed    bool
	Model     string
	TokensIn  int
	TokensOut int
	CacheRead int
	Decisions int
	Decision  *session.Decision
	Context   frame.Context
	SubAgents []subagent.Row
	Diff      string
	Plan      []session.PlanItem
	Created   string
	Agent     string
	Promote   bool
	GateWhy   jev.Why
}

func (e Event) snapshot() bool {
	switch e.Kind {
	case EventContext, EventSubAgent:
		return true
	case EventText, EventTextDelta, EventToolCall, EventToolResult, EventNote, EventFailure, EventStats, EventDone,
		EventDecision, EventGateOff, EventAwaitPerson, EventResumed, EventSteered, EventRequesting, EventPlan,
		EventSession, EventForkStart, EventForkEnd, EventTask, EventStreamReset, EventThinking:
		return false
	}
	panic("tui: unknown event kind")
}

func (e Event) answered() bool {
	switch e.Kind {
	case EventText, EventTextDelta, EventToolCall, EventStats:
		return true
	}
	return false
}

type CalledFromInsideTheTurnAndNeverAfterItReturns func(Event)

type Pick struct {
	Wire   string
	Model  string
	Effort llm.Effort
}

type Turn func(ctx context.Context, pick Pick, task string, emit CalledFromInsideTheTurnAndNeverAfterItReturns)

type Answer int

const (
	Denied Answer = iota
	AllowedOnce
	AlwaysHere
)

type Requirement struct {
	What string
	Fix  string
	Run  func() *exec.Cmd
}

type Wire struct {
	Name     string
	Model    string
	Provider string
	Efforts  []llm.Effort
}

type Options struct {
	Repo         string
	Root         string
	Branch       string
	Note         string
	Release      string
	Requirements []Requirement
	Recheck      func() []Requirement
	Login        func() *exec.Cmd
	Wires        func() []Wire
	Models       func() (library.Library, error)
	Agents       func() isubagent.Found
	Providers    []settings.Provider
	Quota        func() []frame.Quota
	Settings     *isettings.Store
	Promotions   isession.PromotionLog
	Reload       func() string
	Turn         Turn
	Answers      chan<- Answer
	Steering     chan string
	Paste        paste.Board
	Copy         func(text string) error
	Paths        func() []string
	ResumeHead   func() string
	NewSession   func() string
	Now          func() time.Time
	Shells       func() []shells.Entry
	KillShell    func(name string) error
	Fresh        bool
	Resumed      []Event
	Pose         string
	Keymap       string
}

type screen int

const (
	screenChat screen = iota
	screenAgents
	screenEdits
	screenShells
	screenSettings
)

const (
	eventBuffer   = 256
	defaultWidth  = 80
	defaultHeight = 24
	chromeRows    = 3
	bodyTop       = 2
	pulseInterval = session.TickInterval
	noticeShown   = 3 * time.Second
	noticePulses  = int(noticeShown / pulseInterval)
	wheelRows     = 3
	setupPoll     = time.Second
	shellPoll     = 250 * time.Millisecond
	shellPulses   = int(shellPoll / pulseInterval)
	exitReset     = "\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1006l\x1b[?1016l\x1b[0m" + ansi.ResetBackgroundColor
)

type App struct {
	options        Options
	requirements   []Requirement
	current        screen
	view           session.Model
	feed           feed.Model
	edits          edits.Model
	shells         shells.Model
	settings       settings.Model
	store          *isettings.Store
	defaults       []isettings.Spec
	preview        preview
	roles          map[library.RoleID]string
	defined        []isubagent.Definition
	shortcuts      map[string]string
	dialogs        []dialog
	intro          intro
	status         frame.Status
	noticeAt       int
	pulse          int
	pulsing        bool
	filling        bool
	wire           string
	model          string
	provider       string
	picked         string
	chosen         resolvedModel
	effort         llm.Effort
	sessionName    string
	sessionID      string
	wires          []Wire
	width          int
	height         int
	started        time.Time
	busy           bool
	forking        bool
	gateOff        bool
	running        int
	pressedAt      time.Time
	cancel         context.CancelFunc
	events         chan Event
	subAgentCalls  []string
	subAgents      []subagent.Row
	happened       []feed.Event
	feedStale      bool
	reached        []string
	board          paste.Board
	minted         int
	happenedAtTurn int
	keptAnswer     string
	selection      pointer.Selection
	frozen         string
	drag           pointer.Drag
	lastSelection  string
	drawn          string
	hits           []frame.Hit
}

type resolvedModel struct {
	slug  string
	model library.Model
	known bool
}

type Closed struct{}

type requirementsMsg []Requirement

type recheckMsg struct{}

type pathsMsg []string

type pulseMsg struct{}

type fillMsg struct{}

type shellsMsg []shells.Entry

func New(options Options) *App {
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Copy == nil {
		options.Copy = sys.WriteClipboardText
	}
	if options.Models == nil {
		options.Models = shippedModels
	}
	if state, err := sys.ProjectStateDirAt(options.Root); err == nil && options.Promotions == "" && options.Root != "" {
		options.Promotions = isession.NewPromotionLog(state)
	}
	if options.Keymap == "" {
		options.Keymap, _ = keymap.ShortcutsPath()
	}
	app := &App{
		options:      options,
		requirements: options.Requirements,
		view:         session.New(options.Now, new(markdown.Renderer).Lines),
		feed:         feed.New(options.Now),
		edits:        edits.Model{Root: options.Root},
		shells:       shells.New(options.Now),
		settings:     settings.Model{Providers: options.Providers, Scopes: []string{"global", "project"}},
		store:        options.Settings,
		defaults:     isettings.Default(),
		shortcuts:    keymap.LoadShortcuts(options.Keymap),
		width:        defaultWidth,
		height:       defaultHeight,
		started:      options.Now(),
		board:        paste.Default(options.Paste),
	}
	app.status.Note = options.Note
	app.status.Fresh = options.Fresh
	app.view.Commands = commands(options)
	app.settings.SetSearchKey(app.shortcuts[searchAction])
	app.settings.SetBranch(options.Branch)
	app.intro = newIntro(options.Fresh && !app.flag(isettings.HideIntroduction), app.text(isettings.Animations) != animationsOff, options.Pose)
	app.resize(app.width, app.height)
	for _, event := range options.Resumed {
		app.absorb(event)
	}
	app.view.Stop()
	app.readWires()
	app.refreshSettingsRows()
	app.flushFeed()
	app.syncFeed()
	return app
}

func (a *App) readWires() {
	if a.options.Wires == nil {
		return
	}
	signed := a.options.Wires()
	if len(signed) == 0 {
		return
	}
	a.wires = signed
	a.wire, a.model, a.provider = signed[0].Name, signed[0].Model, signed[0].Provider
}

func Run(options Options) error {
	_, err := tea.NewProgram(New(options)).Run()
	_, _ = io.WriteString(os.Stdout, exitReset)
	return err
}

func (a *App) Init() tea.Cmd {
	return tea.Batch(a.view.Focus(), a.intro.start(), a.pollQuota(), a.readPaths(), a.watchSetup(), a.pollShells(), a.startPulse())
}

func (a *App) pollShells() tea.Cmd {
	poll := a.options.Shells
	if poll == nil {
		return nil
	}
	return func() tea.Msg { return shellsMsg(poll()) }
}

func (a *App) watchSetup() tea.Cmd {
	if len(a.requirements) == 0 || a.options.Recheck == nil {
		return nil
	}
	return tea.Tick(setupPoll, func(time.Time) tea.Msg { return recheckMsg{} })
}

func (a *App) readPaths() tea.Cmd {
	under := a.options.Paths
	if under == nil {
		return nil
	}
	return func() tea.Msg { return pathsMsg(under()) }
}

func (a *App) pollQuota() tea.Cmd {
	poll := a.options.Quota
	if poll == nil {
		return nil
	}
	return func() tea.Msg { return poll() }
}

func (a *App) resize(width, height int) {
	a.width, a.height = width, height
	body := height - chromeRows
	a.view.SetSize(width, body)
	a.feed.SetSize(width, body)
	a.edits.SetSize(width, body)
	a.shells.SetSize(width, body)
	a.settings.SetSize(width, height)
	a.intro.resize(width)
	a.selection, a.frozen = pointer.Selection{}, ""
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := a.update(msg)
	a.flushFeed()
	a.syncFeed()
	return a, tea.Batch(cmd, a.startPulse())
}

func (a *App) update(msg tea.Msg) tea.Cmd {
	if size, resized := msg.(tea.WindowSizeMsg); resized {
		a.resize(size.Width, size.Height)
		cmd, _ := a.toFiles(msg)
		return tea.Batch(cmd, a.fillLater())
	}
	if a.intro.shown && len(a.requirements) == 0 {
		if cmd, taken := a.coverInput(msg); taken {
			return cmd
		}
	}
	if a.hostResult(msg) {
		return nil
	}
	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		a.press(msg)
		return nil
	case tea.MouseMotionMsg:
		a.motion(msg)
		return nil
	case tea.MouseReleaseMsg:
		return a.release(msg)
	case tea.MouseWheelMsg:
		return a.wheel(msg)
	case tea.KeyPressMsg:
		return a.key(msg)
	case tea.PasteMsg:
		return a.pasted(msg)
	case pulseMsg:
		a.beat()
		if a.busy && a.pulse%shellPulses == 0 {
			return a.pollShells()
		}
		return nil
	case fillMsg:
		a.filling = false
		if a.view.Fill() {
			return a.fillLater()
		}
		return nil
	case Event:
		a.absorb(msg)
		if a.events == nil {
			return nil
		}
		return a.waitForEvent()
	case paste.Outcome:
		a.view.Attached(msg)
		return nil
	case copiedMsg:
		a.notify(msg.note())
		if msg.state == copyToTerminal {
			return tea.SetClipboard(msg.text)
		}
		return nil
	case Closed:
		return a.closed()
	case pathsMsg:
		a.view.Paths = msg
		return nil
	case []frame.Quota:
		a.status.Quotas = msg
		return nil
	case recheckMsg:
		recheck := a.options.Recheck
		if recheck == nil {
			return nil
		}
		return func() tea.Msg { return requirementsMsg(recheck()) }
	case requirementsMsg:
		cleared := len(a.requirements) > 0 && len(msg) == 0
		a.requirements = msg
		a.readWires()
		if !cleared {
			return a.watchSetup()
		}
		return tea.Batch(a.view.Focus(), a.intro.start())
	case shellsMsg:
		a.showShells(msg)
		return nil
	}
	if cmd, open := a.toFiles(msg); open {
		return cmd
	}
	if a.intro.shown {
		return a.intro.animate(msg)
	}
	return a.view.Update(msg)
}

func (a *App) startPulse() tea.Cmd {
	moving := a.busy && !a.view.Awaiting() || a.status.Note != ""
	if a.pulsing || !moving {
		return nil
	}
	a.pulsing = true
	return tea.Tick(pulseInterval, func(time.Time) tea.Msg { return pulseMsg{} })
}

func (a *App) fillLater() tea.Cmd {
	if a.filling {
		return nil
	}
	a.filling = true
	return tea.Tick(konst.RewrapFillMillis*time.Millisecond, func(time.Time) tea.Msg { return fillMsg{} })
}

func (a *App) beat() {
	a.pulsing = false
	a.pulse++
	if a.text(isettings.Animations) != animationsOff {
		a.feed.SetFrame(a.pulse)
		a.view.SetFrame(a.pulse)
	}
	if a.status.Note != "" && a.pulse-a.noticeAt > noticePulses {
		a.status.Note = ""
	}
}

func (a *App) notify(note string) {
	a.status.Note, a.noticeAt = note, a.pulse
}

func (a *App) show(to screen) tea.Cmd {
	if to != screenSettings && a.settings.CloseDialog().Action == settings.ActionRevert {
		a.preview = preview{}
	}
	a.current = to
	a.intro.settings = a.intro.settings && to == screenSettings
	return a.clearDialogs()
}

func (a *App) clearDialogs() tea.Cmd {
	a.dialogs = nil
	if a.current == screenChat {
		return a.view.Focus()
	}
	return nil
}

func (a *App) closed() tea.Cmd {
	a.busy, a.cancel, a.events, a.edits.Busy = false, nil, nil, false
	a.running, a.pressedAt = 0, time.Time{}
	a.parkSubAgentsTheTurnLeftBehind()
	a.stopWhatStillRuns()
	a.view.Stop()
	a.dropSteering()
	next := tea.Batch(a.pollQuota(), a.readPaths(), a.pollShells())
	if task, queued := a.view.Release(); queued {
		return tea.Batch(a.start(task), next)
	}
	return next
}
