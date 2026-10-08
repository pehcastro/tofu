package tui

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/edits"
	"tofu/interface/tui/feed"
	"tofu/interface/tui/frame"
	"tofu/interface/tui/keyfield"
	"tofu/interface/tui/markdown"
	"tofu/interface/tui/paste"
	"tofu/interface/tui/pointer"
	"tofu/interface/tui/session"
	"tofu/interface/tui/settings"
	"tofu/interface/tui/shells"
	"tofu/interface/tui/subagent"
	"tofu/internal/host"
	"tofu/internal/keymap"
	"tofu/internal/konst"
	"tofu/internal/llm"
	library "tofu/internal/llm/models"
	isession "tofu/internal/session"
	isettings "tofu/internal/settings"
	isubagent "tofu/internal/subagent"
	"tofu/internal/sys"
)

type EventKind = host.EventKind

const (
	EventText        = host.EventText
	EventTextDelta   = host.EventTextDelta
	EventToolCall    = host.EventToolCall
	EventToolResult  = host.EventToolResult
	EventNote        = host.EventNote
	EventFailure     = host.EventFailure
	EventStats       = host.EventStats
	EventDone        = host.EventDone
	EventDecision    = host.EventDecision
	EventGateOff     = host.EventGateOff
	EventContext     = host.EventContext
	EventForkStart   = host.EventForkStart
	EventForkEnd     = host.EventForkEnd
	EventSubAgent    = host.EventSubAgent
	EventAwaitPerson = host.EventAwaitPerson
	EventResumed     = host.EventResumed
	EventSteered     = host.EventSteered
	EventRequesting  = host.EventRequesting
	EventPlan        = host.EventPlan
	EventSession     = host.EventSession
	EventTask        = host.EventTask
	EventStreamReset = host.EventStreamReset
	EventThinking    = host.EventThinking
	EventTurnStarted = host.EventTurnStarted
	EventPersisted   = host.EventPersisted
)

type Event = host.Event

func answered(e Event) bool {
	switch e.Kind {
	case EventText, EventTextDelta, EventToolCall, EventThinking, EventStats:
		return true
	}
	return false
}

type CalledFromInsideTheTurnAndNeverAfterItReturns func(Event)

type Pick = host.Pick

type Turn func(ctx context.Context, pick Pick, task string, emit CalledFromInsideTheTurnAndNeverAfterItReturns)

type Answer = host.Answer

const (
	Denied      = host.Denied
	AllowedOnce = host.AllowedOnce
	AlwaysHere  = host.AlwaysHere
)

type Requirement struct {
	Step    string
	What    string
	Fix     string
	Run     func() *exec.Cmd
	Done    string
	Choices []Choice
}

type Choice struct {
	Label string
	Run   func() *exec.Cmd
	Key   string
}

type setupEntry struct {
	label, variable, refusal string
	field                    keyfield.Field
	checking                 int
}

type keySavedMsg struct {
	check int
	text  string
	err   error
}

type Wire struct {
	Name     string
	Model    string
	Provider string
	Efforts  []llm.Effort
}

type Options struct {
	Repo          string
	Root          string
	Branch        string
	Note          string
	Release       string
	Requirements  []Requirement
	Recheck       func() []Requirement
	SaveKey       func(ctx context.Context, variable, value string) (string, error)
	Login         func() *exec.Cmd
	Wires         func() []Wire
	Models        func() (library.Library, error)
	Agents        func() isubagent.Found
	Providers     []settings.Provider
	Quota         func() []frame.Quota
	Settings      *isettings.Store
	Promotions    isession.PromotionLog
	Reload        func() string
	ReloadModels  func() string
	ModelsStale   bool
	Host          *host.Host
	Turn          Turn
	Paste         paste.Board
	Copy          func(text string) error
	Paths         func() []string
	Sessions      func() ([]SessionRow, error)
	Resume        func(id string) (string, []Event)
	NewSession    func() string
	Compact       func() string
	Undo          func(count string) string
	Updates       func() string
	Now           func() time.Time
	Shells        func() []shells.Entry
	KillShell     func(name string) error
	RunCommand    func(ctx context.Context, command string) (output string, stopped bool)
	Fresh         bool
	Resumed       []Event
	Pose          string
	Keymap        string
	PromptHistory string
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

const (
	termWithoutHardTabs  = "TERM=linux"
	controlSequenceStart = "\x1b["
	absoluteColumnFinal  = '`'
	columnFromStartFinal = 'G'
)

type App struct {
	options        Options
	requirements   []Requirement
	entry          setupEntry
	keyChecks      int
	setupNote      string
	current        screen
	view           session.Model
	feed           feed.Model
	edits          edits.Model
	shells         shells.Model
	settings       settings.Model
	store          *isettings.Store
	defaults       []isettings.Spec
	preview        preview
	roles          map[library.RoleID]library.Role
	defined        []isubagent.Definition
	shortcuts      map[string]string
	prompts        *isession.PromptHistory
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
	sessionRoot    string
	wires          []Wire
	width          int
	height         int
	started        time.Time
	busy           bool
	leading        bool
	afterTurn      []func() string
	reports        map[string]string
	heldReports    []subagent.Row
	forking        bool
	gateOff        bool
	running        int
	pressedAt      time.Time
	stopCommand    context.CancelFunc
	subAgentCalls  []string
	subAgents      []subagent.Row
	moves          map[string]movement
	liveShells     []shells.Entry
	shellsTicking  bool
	happened       []feed.Event
	happenedIndex  map[string]int
	feedStale      bool
	reached        []string
	board          paste.Board
	minted         int
	happenedAtTurn int
	keptAnswer     string
	updateSaid     string
	selection      pointer.Selection
	frozen         string
	drag           pointer.Drag
	lastSelection  string
	drawn          string
	hits           []frame.Hit
	listening      atomic.Bool
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

type shellsDueMsg struct{}

type modelsReloadedMsg string

type updateMsg string

func New(options Options) *App {
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Copy == nil {
		options.Copy = sys.WriteClipboardText
	}
	if options.Models == nil {
		options.Models = layeredModels(options.Root)
	}
	if state, err := sys.ProjectStateDirAt(options.Root); err == nil && options.Promotions == "" && options.Root != "" {
		options.Promotions = isession.NewPromotionLog(state)
	}
	if options.Keymap == "" {
		options.Keymap, _ = keymap.ShortcutsPath()
	}
	if options.PromptHistory == "" {
		options.PromptHistory, _ = sys.HomeConfigDir()
	}
	if options.Host == nil && options.Turn != nil {
		turn := options.Turn
		options.Host, _ = host.New(host.Config{Now: options.Now, Play: func(ctx context.Context, pick Pick, task string, live host.Live) {
			turn(ctx, pick, task, live.Emit)
		}})
	}
	app := &App{
		options:       options,
		requirements:  options.Requirements,
		view:          session.New(options.Now, new(markdown.Renderer).Lines),
		feed:          feed.New(options.Now),
		edits:         edits.Model{Root: options.Root},
		shells:        shells.New(options.Now),
		settings:      settings.Model{Providers: options.Providers, Scopes: []string{"global", "project"}},
		store:         options.Settings,
		defaults:      isettings.Default(),
		shortcuts:     keymap.LoadShortcuts(options.Keymap),
		width:         defaultWidth,
		height:        defaultHeight,
		started:       options.Now(),
		reports:       map[string]string{},
		moves:         map[string]movement{},
		happenedIndex: map[string]int{},
		board:         paste.Default(options.Paste),
	}
	if options.PromptHistory != "" {
		app.prompts = isession.OpenPromptHistory(options.PromptHistory)
	}
	app.status.Note = options.Note
	app.status.Fresh = options.Fresh
	app.view.Commands = commands(options)
	app.settings.SetSearchKey(app.shortcuts[searchAction])
	app.settings.SetBranch(options.Branch)
	app.intro = newIntro(options.Fresh, app.text(isettings.Animations) != animationsOff, options.Pose)
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

func consoleEnviron(environ []string, goos string) []string {
	if goos != "windows" {
		return environ
	}
	return append(slices.DeleteFunc(slices.Clone(environ), func(entry string) bool { return strings.HasPrefix(entry, "TERM=") }), termWithoutHardTabs)
}

type conhostOutput struct{ *os.File }

func (c conhostOutput) Write(frame []byte) (int, error) {
	if _, err := c.File.Write(columnMovesAsCHA(frame)); err != nil {
		return 0, err
	}
	return len(frame), nil
}

func columnMovesAsCHA(frame []byte) []byte {
	out := make([]byte, 0, len(frame))
	for {
		at := bytes.Index(frame, []byte(controlSequenceStart))
		if at < 0 {
			return append(out, frame...)
		}
		end := at + len(controlSequenceStart)
		for end < len(frame) && frame[end] >= '0' && frame[end] <= '9' {
			end++
		}
		out = append(out, frame[:end]...)
		if end < len(frame) && frame[end] == absoluteColumnFinal {
			out, end = append(out, columnFromStartFinal), end+1
		}
		frame = frame[end:]
	}
}

func Run(options Options) error {
	environ := os.Environ()
	profile := colorprofile.Detect(os.Stdout, environ)
	programOptions := []tea.ProgramOption{tea.WithEnvironment(consoleEnviron(environ, runtime.GOOS)), tea.WithColorProfile(profile)}
	if runtime.GOOS == "windows" {
		programOptions = append(programOptions, tea.WithOutput(conhostOutput{os.Stdout}))
	}
	program := tea.NewProgram(New(options), programOptions...)
	_, err := program.Run()
	_, _ = io.WriteString(os.Stdout, exitReset)
	return err
}

func (a *App) Init() tea.Cmd {
	return tea.Batch(a.view.Focus(), a.intro.start(), a.pollQuota(), a.readPaths(), a.watchSetup(), a.pollShells(), a.startPulse(), a.reloadStaleModels(), a.listen(), a.watchUpdates(0))
}

func (a *App) watchUpdates(after time.Duration) tea.Cmd {
	updates := a.options.Updates
	if updates == nil {
		return nil
	}
	return tea.Tick(after, func(time.Time) tea.Msg { return updateMsg(updates()) })
}

func (a *App) reloadStaleModels() tea.Cmd {
	reload := a.options.ReloadModels
	if !a.options.ModelsStale || reload == nil {
		return nil
	}
	return func() tea.Msg { return modelsReloadedMsg(reload()) }
}

func (a *App) pollShells() tea.Cmd {
	poll := a.options.Shells
	if poll == nil {
		return nil
	}
	return func() tea.Msg { return shellsMsg(poll()) }
}

func (a *App) watchSetup() tea.Cmd {
	if !a.settingUp() || a.options.Recheck == nil {
		return nil
	}
	return tea.Tick(setupPoll, func(time.Time) tea.Msg { return recheckMsg{} })
}

func (a *App) settingUp() bool {
	return slices.ContainsFunc(a.requirements, func(step Requirement) bool { return step.Done == "" })
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
	a.selection, a.frozen = pointer.Selection{}, ""
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := a.update(msg)
	if a.options.Host != nil {
		a.options.Host.Choose(Pick{Wire: a.wire, Model: a.picked, Effort: a.effort})
	}
	a.view.Activity = nil
	if a.busy {
		a.watchSubAgents()
		a.view.Activity = append(a.view.Activity, a.shellActivity()...)
	}
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
		if a.busy && !a.shellsTicking && a.pulse%shellPulses == 0 {
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
		return a.listen()
	case paste.Outcome:
		a.view.Attached(msg)
		return nil
	case editedMsg:
		a.edited(msg)
		return nil
	case ranMsg:
		a.ran(msg)
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
	case keySavedMsg:
		return a.keySaved(msg)
	case requirementsMsg:
		was, before := a.settingUp(), a.currentStep()
		a.requirements = msg
		cleared := was && !a.settingUp()
		a.readWires()
		switch {
		case cleared:
			return tea.Batch(a.view.Focus(), tea.ClearScreen)
		case a.currentStep().Step != before.Step || a.currentStep().What != before.What:
			return tea.Batch(a.watchSetup(), tea.ClearScreen)
		}
		return a.watchSetup()
	case shellsMsg:
		a.showShells(msg)
		if a.shellsTicking || !slices.ContainsFunc(msg, func(entry shells.Entry) bool { return entry.State == shells.Running }) {
			return nil
		}
		a.shellsTicking = true
		return tea.Tick(shellPoll, func(time.Time) tea.Msg { return shellsDueMsg{} })
	case shellsDueMsg:
		a.shellsTicking = false
		return a.pollShells()
	case pickerReloadedMsg:
		return a.pickerReloaded(string(msg))
	case modelsReloadedMsg:
		if msg != "" {
			a.notify(string(msg))
		}
		return nil
	case updateMsg:
		if msg != "" && string(msg) != a.updateSaid {
			a.updateSaid = string(msg)
			a.view.Append(session.Entry{Kind: session.Note, Body: a.updateSaid})
		}
		return a.watchUpdates(konst.UpdateWatchSeconds * time.Second)
	}
	if cmd, open := a.toFiles(msg); open {
		return cmd
	}
	if a.intro.shown {
		var moved tea.Cmd
		a.intro.identity, moved = a.intro.identity.Update(msg)
		return tea.Batch(moved, a.view.Update(msg))
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
	a.busy, a.leading, a.edits.Busy = false, false, false
	a.running, a.pressedAt = 0, time.Time{}
	a.parkSubAgentsTheTurnLeftBehind()
	a.drawHeldReports()
	a.stopWhatStillRuns()
	a.view.Stop()
	a.dropSteering()
	a.countCrons()
	a.carryHeld()
	next := tea.Batch(a.pollQuota(), a.readPaths(), a.pollShells(), a.listen())
	if task, queued := a.view.Release(); queued {
		a.start(task)
	}
	return next
}
