package tui

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"tofu/interface/tui/crew"
	"tofu/interface/tui/edits"
	"tofu/interface/tui/frame"
	"tofu/interface/tui/markdown"
	"tofu/interface/tui/paste"
	"tofu/interface/tui/session"
	"tofu/interface/tui/settings"
	"tofu/interface/tui/theme"
	"tofu/internal/sys"
	"tofu/internal/widget"
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
	EventCrew
	EventAwaitPerson
	EventResumed
	EventSteered
	EventRequesting
	EventPlan
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
	Children  []crew.Child
	Diff      string
	Plan      []session.PlanItem
	Created   string
	Agent     string
}

func (e Event) snapshot() bool {
	switch e.Kind {
	case EventContext, EventForkStart, EventForkEnd, EventCrew:
		return true
	case EventText, EventTextDelta, EventToolCall, EventToolResult, EventNote, EventFailure, EventStats, EventDone,
		EventDecision, EventGateOff, EventAwaitPerson, EventResumed, EventSteered, EventRequesting, EventPlan:
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

type Turn func(ctx context.Context, wire, task string, emit func(Event))

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
}

type Options struct {
	Repo         string
	Branch       string
	Note         string
	Release      string
	Requirements []Requirement
	Recheck      func() []Requirement
	Login        func() *exec.Cmd
	Wires        func() []Wire
	Providers    []settings.Provider
	Quota        func(wire string) frame.Quota
	Turn         Turn
	Answers      chan<- Answer
	Steering     chan string
	Paste        paste.Board
	Copy         func(text string) error
	Paths        func() []string
	ResumeHead   func() string
	NewSession   func() string
	Now          func() time.Time
}

type viewID int

const (
	viewSession viewID = iota
	viewCrew
	viewEdits
	viewSettings
)

func namedViews() []frame.View {
	return []frame.View{
		{Digit: '1', Name: "session"},
		{Digit: '2', Name: "crew"},
		{Digit: '3', Name: "file edits"},
		{Digit: '6', Name: "settings"},
	}
}

const (
	eventBuffer   = 256
	defaultWidth  = 80
	defaultHeight = 24
	viewChrome    = 3
	headerRows    = 1
	stripRow      = 1
	bodyRow       = stripRow + 1
	setupTitle    = "tofu cannot start a turn yet"
	setupKeys     = "[1-9] run the fix   [r] check again   [q] quit"
	setupIndent   = "   "
	setupWatch    = "or run the command in another terminal: tofu picks it up here"
	setupPoll     = time.Second
	readyNote     = "type a task and press enter. tofu works in "
	gateOffLine   = "the gate is off, so no call on this session is judged."
	altPrefix     = "alt+"
	stoppingNote  = "stopping the turn"
	droppedQueue  = ", and the queue with it"
)

type App struct {
	options      Options
	requirements []Requirement
	current      viewID
	strip        frame.Strip
	view         session.Model
	crew         crew.Model
	edits        edits.Model
	settings     settings.Model
	status       frame.Status
	wire         string
	model        string
	provider     string
	width        int
	height       int
	started      time.Time
	busy         bool
	ticking      bool
	gateOff      bool
	cancel       context.CancelFunc
	events       chan Event
	board        paste.Board
}

type closedMsg struct{}

type requirementsMsg []Requirement

type recheckMsg struct{}

type pathsMsg []string

type tickMsg time.Time

func New(options Options) *App {
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Copy == nil {
		options.Copy = sys.WriteClipboardText
	}
	if options.Release == "" {
		options.Release = frame.Release(sys.Version(), sys.BuildRevision())
	}
	app := &App{
		options:      options,
		requirements: options.Requirements,
		strip:        frame.Strip{Views: namedViews()},
		view:         session.New(options.Now, new(markdown.Renderer).Lines),
		settings:     settings.Model{Providers: options.Providers},
		width:        defaultWidth,
		height:       defaultHeight,
		started:      options.Now(),
		board:        paste.Default(options.Paste),
	}
	app.status.Note = options.Note
	app.view.Commands = commands(options)
	app.resize(app.width, app.height)
	app.readWires()
	if len(app.requirements) == 0 {
		app.sayWhatToType()
	}
	return app
}

func (a *App) sayWhatToType() {
	a.view.Append(session.Entry{Kind: session.Note, Body: readyNote + a.options.Repo})
}

func (a *App) readWires() {
	if a.options.Wires == nil {
		return
	}
	if signed := a.options.Wires(); len(signed) > 0 {
		a.wire, a.model, a.provider = signed[0].Name, signed[0].Model, signed[0].Provider
	}
}

func Run(options Options) error {
	_, err := tea.NewProgram(New(options)).Run()
	return err
}

func (a *App) Init() tea.Cmd {
	return tea.Batch(a.view.Focus(), a.pollQuota(), a.readPaths(), a.watchSetup())
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

func (a *App) resize(width, height int) {
	a.width, a.height = width, height
	a.view.SetSize(width, height-viewChrome)
	a.crew.SetSize(width, height-viewChrome)
	a.edits.SetSize(width, height-viewChrome)
	a.settings.SetSize(width, height-viewChrome)
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.resize(msg.Width, msg.Height)
		return a, nil

	case tea.KeyPressMsg:
		return a.key(msg)

	case tea.MouseClickMsg:
		if msg.Y == stripRow && len(a.requirements) == 0 {
			if index, hit := a.strip.Hit(msg.X); hit {
				a.show(viewID(index))
			}
		}
		return a, nil

	case tea.MouseWheelMsg:
		if a.current != viewSession || len(a.requirements) > 0 {
			return a, nil
		}
		switch msg.Button {
		case tea.MouseWheelUp:
			a.view.Scroll(session.WheelUp)
		case tea.MouseWheelDown:
			a.view.Scroll(session.WheelDown)
		}
		return a, nil

	case Event:
		a.absorb(msg)
		if a.events == nil {
			return a, a.tick()
		}
		return a, tea.Batch(a.waitForEvent(), a.tick())

	case paste.Outcome:
		a.view.Attached(msg)
		return a, nil

	case copiedMsg:
		a.view.Append(msg.entry())
		if msg.state == copyToTerminal {
			return a, tea.SetClipboard(msg.text)
		}
		return a, nil

	case closedMsg:
		a.busy, a.cancel, a.events, a.edits.Busy = false, nil, nil, false
		a.view.Stop()
		a.dropSteering()
		next := tea.Batch(a.pollQuota(), a.readPaths())
		if task, queued := a.view.Release(); queued {
			return a, tea.Batch(a.start(task), next)
		}
		return a, next

	case pathsMsg:
		a.view.Paths = msg
		return a, nil

	case frame.Quota:
		a.status.Quota = msg
		return a, nil

	case recheckMsg:
		recheck := a.options.Recheck
		if recheck == nil {
			return a, nil
		}
		return a, func() tea.Msg { return requirementsMsg(recheck()) }

	case requirementsMsg:
		cleared := len(a.requirements) > 0 && len(msg) == 0
		a.requirements = msg
		a.readWires()
		if !cleared {
			return a, a.watchSetup()
		}
		a.sayWhatToType()
		return a, a.view.Focus()

	case tickMsg:
		a.ticking = false
		return a, a.tick()
	}
	return a, a.view.Update(msg)
}

func jump(key string) (viewID, bool) {
	for index, view := range namedViews() {
		if key == string(view.Digit) {
			return viewID(index), true
		}
	}
	return viewSession, false
}

func (a *App) show(view viewID) {
	a.current = view
	a.strip.Current = int(view)
}

func (a *App) step(by int) {
	views := len(namedViews())
	a.show(viewID((int(a.current) + by + views) % views))
}

func (a *App) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if len(a.requirements) > 0 {
		return a.setupKey(key)
	}
	if key == "ctrl+c" {
		if !a.busy {
			return a, tea.Quit
		}
		if a.view.Stopping {
			return a, nil
		}
		a.view.Stopping = true
		a.cancel()
		note := stoppingNote
		a.dropSteering()
		if a.view.DropQueue() {
			note = stoppingNote + droppedQueue
		}
		a.view.Append(session.Entry{Kind: session.Note, Body: note})
		return a, nil
	}
	if a.view.Awaiting() {
		a.answer(key)
		return a, nil
	}
	if a.current == viewSession {
		if handled, cmd := a.menuKey(key); handled {
			return a, cmd
		}
	}
	switch key {
	case "tab":
		a.step(1)
		return a, nil
	case "shift+tab":
		a.step(-1)
		return a, nil
	case "esc":
		a.show(viewSession)
		return a, nil
	case "ctrl+o":
		a.view.ToggleOpen()
		return a, nil
	case "ctrl+v", "alt+v":
		if a.current != viewSession {
			return a, nil
		}
		return a, a.view.Paste(a.board)
	}
	digit, alt := strings.CutPrefix(key, altPrefix)
	if alt || a.current != viewSession {
		if jumped, ok := jump(digit); ok {
			a.show(jumped)
			return a, nil
		}
	}
	switch a.current {
	case viewCrew:
		a.crew.Key(key)
		return a, nil
	case viewEdits:
		a.edits.Key(key)
		return a, nil
	case viewSettings:
		return a, nil
	case viewSession:
	}
	switch key {
	case "enter":
		if name, asked := a.view.Command(); asked {
			return a, a.runCommand(name)
		}
		return a, a.send()
	case "ctrl+x":
		a.view.Unqueue()
		return a, nil
	case "alt+up":
		a.view.PickQueued(-1)
		return a, nil
	case "alt+down":
		a.view.PickQueued(1)
		return a, nil
	case "ctrl+y":
		return a, a.copyAnswer()
	case "alt+y":
		call, found := a.view.LastCall()
		return a, a.copy(callUnit, call, found)
	}
	if a.view.Scroll(key) {
		return a, nil
	}
	return a, a.view.Update(msg)
}

func (a *App) answer(key string) {
	var answer Answer
	switch key {
	case "a":
		answer = AllowedOnce
	case "d":
		answer = Denied
	case "A":
		answer = AlwaysHere
	default:
		return
	}
	if a.options.Answers == nil {
		return
	}
	select {
	case a.options.Answers <- answer:
		a.view.Resume()
	default:
	}
}

func (a *App) setupKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q", "ctrl+c":
		return a, tea.Quit
	case "r":
		return a, func() tea.Msg { return a.checkedRequirements() }
	}
	index, err := strconv.Atoi(key)
	if err != nil || index < 1 || index > len(a.requirements) {
		return a, nil
	}
	run := a.requirements[index-1].Run
	if run == nil {
		run = a.options.Login
	}
	if run == nil {
		return a, nil
	}
	return a, tea.ExecProcess(run(), func(error) tea.Msg { return a.checkedRequirements() })
}

func (a *App) send() tea.Cmd {
	task := a.view.Value()
	if task == "" {
		return nil
	}
	a.view.Reset()
	if a.busy {
		a.view.Queue(task)
		a.steer(task)
		return nil
	}
	a.view.Append(session.Entry{Kind: session.User, Body: task})
	return a.start(task)
}

func (a *App) steer(task string) {
	select {
	case a.options.Steering <- task:
	default:
	}
}

func (a *App) dropSteering() {
	for {
		select {
		case <-a.options.Steering:
		default:
			return
		}
	}
}

func (a *App) start(task string) tea.Cmd {
	a.view.Follow()
	if a.options.Turn == nil {
		a.view.Append(session.Entry{Kind: session.Failure, Body: "no engine is wired to this app"})
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan Event, eventBuffer)
	a.busy, a.cancel, a.events, a.edits.Busy = true, cancel, events, true
	a.view.Start()
	turn, wire := a.options.Turn, a.wire
	deliver := func(event Event) {
		if !event.snapshot() {
			events <- event
			return
		}
		select {
		case events <- event:
		default:
		}
	}
	go func() {
		turn(ctx, wire, task, deliver)
		cancel()
		close(events)
	}()
	return tea.Batch(a.waitForEvent(), a.tick())
}

func (a *App) waitForEvent() tea.Cmd {
	events := a.events
	return func() tea.Msg {
		event, open := <-events
		if !open {
			return closedMsg{}
		}
		return event
	}
}

func (a *App) absorb(event Event) {
	if event.answered() {
		a.view.Returned()
	}
	switch event.Kind {
	case EventRequesting:
		a.view.Requesting()
	case EventText:
		a.view.Append(session.Entry{Kind: session.Assistant, Body: event.Text})
	case EventTextDelta:
		a.view.Stream(event.Text)
	case EventToolCall:
		a.view.Append(session.Entry{Kind: session.Tool, ID: event.ID, Head: event.Tool, Body: event.Text, Detail: event.Detail})
	case EventToolResult:
		status := event.Text
		if edit, changed := edits.Changed(event.Agent, a.view.Intent(event.ID), event.Diff, event.Created); changed {
			a.edits.Add(edit)
			status = edit.Tally()
		}
		a.view.Finish(event.ID, session.Result{Status: status, Bytes: event.Bytes, Failed: event.Failed})
	case EventNote:
		a.view.Append(session.Entry{Kind: session.Note, Body: event.Text})
	case EventDone:
		a.view.Close(event.Text)
	case EventFailure:
		a.view.Append(session.Entry{Kind: session.Failure, Body: event.Text})
	case EventDecision:
		if event.Decision != nil {
			a.view.Decide(*event.Decision)
		}
	case EventGateOff:
		if !a.gateOff {
			a.gateOff = true
			a.view.Append(session.Entry{Kind: session.Note, Body: strings.TrimSpace(gateOffLine + " " + event.Text)})
		}
	case EventContext:
		a.status.Context = event.Context
	case EventCrew:
		a.crew.Children, a.view.Children, a.edits.Children = event.Children, event.Children, event.Children
		a.status.Agents = a.crew.Running()
	case EventPlan:
		a.view.SetPlan(event.Plan)
	case EventAwaitPerson:
		a.view.Await()
	case EventResumed:
		a.view.Resume()
	case EventSteered:
		a.view.Delivered(event.Text)
	case EventForkStart:
		a.strip.Notice = frame.ForkNotice
	case EventForkEnd:
		a.strip.Notice = ""
	case EventStats:
		a.status.TokensIn, a.status.TokensOut, a.status.CacheRead = event.TokensIn, event.TokensOut, event.CacheRead
		a.status.Decisions = event.Decisions
		if event.Model != "" {
			a.model = event.Model
		}
	}
}

func (a *App) checkedRequirements() requirementsMsg {
	if a.options.Recheck == nil {
		return requirementsMsg(a.requirements)
	}
	return requirementsMsg(a.options.Recheck())
}

func (a *App) pollQuota() tea.Cmd {
	poll, wire := a.options.Quota, a.wire
	if poll == nil {
		return nil
	}
	return func() tea.Msg { return poll(wire) }
}

func (a *App) tick() tea.Cmd {
	if a.ticking || !a.busy {
		return nil
	}
	a.ticking = true
	return tea.Tick(session.TickInterval, func(at time.Time) tea.Msg { return tickMsg(at) })
}

func (a *App) View() tea.View {
	at := a.options.Now()
	head := frame.Head{
		Release:  a.options.Release,
		Repo:     a.options.Repo,
		Branch:   a.options.Branch,
		Provider: a.provider,
		Model:    a.model,
		At:       at,
		Elapsed:  at.Sub(a.started),
	}
	rows := []string{frame.Header(head, a.width)}
	var caret *tea.Cursor
	if len(a.requirements) > 0 {
		rows = append(rows, a.setupView(a.height-headerRows))
	} else {
		status := a.status
		status.At = at
		rows = append(rows, a.strip.Render(a.width), a.body(), frame.Bar(status, a.width))
		if a.current == viewSession {
			caret = a.view.Cursor()
		}
	}
	if caret != nil {
		caret.Y += bodyRow
	}
	view := tea.NewView(lipgloss.JoinVertical(lipgloss.Left, rows...))
	view.AltScreen = true
	view.MouseMode = tea.MouseModeNone
	view.Cursor = caret
	return view
}

func (a *App) body() string {
	switch a.current {
	case viewSession:
		return a.view.View()
	case viewCrew:
		return a.crew.View()
	case viewEdits:
		return a.edits.View()
	case viewSettings:
		return a.settings.View()
	}
	panic("tui: unknown view")
}

func (a *App) setupView(rows int) string {
	lines := []string{theme.Warn().Render(widget.Fit(setupTitle, a.width)), ""}
	for index, requirement := range a.requirements {
		number := strconv.Itoa(index + 1)
		for _, line := range widget.Wrap(number+". "+requirement.What, a.width) {
			lines = append(lines, theme.Text().Render(line))
		}
		fix := "press " + number + " to run   " + requirement.Fix
		for _, line := range widget.Wrap(fix, a.width-widget.Cells(setupIndent)) {
			lines = append(lines, theme.Dim().Render(setupIndent+line))
		}
		lines = append(lines, "")
	}
	if a.options.Recheck != nil {
		lines = append(lines, theme.Dim().Render(widget.Fit(setupWatch, a.width)), "")
	}
	lines = append(lines, theme.Faint().Render(widget.Fit(setupKeys, a.width)))
	for len(lines) < rows {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}
