package tui

import (
	"cmp"
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
	"tofu/interface/tui/links"
	"tofu/interface/tui/markdown"
	"tofu/interface/tui/models"
	"tofu/interface/tui/paste"
	"tofu/interface/tui/pick"
	"tofu/interface/tui/quote"
	"tofu/interface/tui/session"
	"tofu/interface/tui/settings"
	"tofu/interface/tui/shells"
	"tofu/interface/tui/theme"
	"tofu/interface/tui/trace"
	"tofu/interface/tui/work"
	library "tofu/internal/llm/models"
	isession "tofu/internal/session"
	isettings "tofu/internal/settings"
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
	EventSession
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
	Promote   bool
}

func (e Event) snapshot() bool {
	switch e.Kind {
	case EventContext, EventForkStart, EventForkEnd, EventCrew:
		return true
	case EventText, EventTextDelta, EventToolCall, EventToolResult, EventNote, EventFailure, EventStats, EventDone,
		EventDecision, EventGateOff, EventAwaitPerson, EventResumed, EventSteered, EventRequesting, EventPlan, EventSession:
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

type Turn func(ctx context.Context, wire, task string, emit CalledFromInsideTheTurnAndNeverAfterItReturns)

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
	Root         string
	Branch       string
	Note         string
	Release      string
	Requirements []Requirement
	Recheck      func() []Requirement
	Login        func() *exec.Cmd
	Wires        func() []Wire
	Models       func() (library.Library, error)
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
}

type viewID int

const (
	viewChat viewID = iota
	viewWork
	viewEdits
	viewCrew
	viewShells
	viewSettings
	viewLinks
	viewQuote
	viewModels
)

const subAgentsIndex = int(viewCrew)

func namedViews() []frame.View {
	return []frame.View{
		{Digit: '1', Name: "chat"},
		{Digit: '2', Name: "work"},
		{Digit: '3', Name: "file edits"},
		{Digit: '4', Name: subAgentsLabel(0)},
		{Digit: '5', Name: "shells"},
	}
}

func subAgentsLabel(running int) string {
	if running == 0 {
		return "sub-agents"
	}
	return "sub-agents (" + strconv.Itoa(running) + ")"
}

const (
	eventBuffer    = 256
	defaultWidth   = 80
	defaultHeight  = 24
	viewChrome     = 3
	headerRows     = 1
	stripRow       = 1
	bodyRow        = stripRow + 1
	setupTitle     = "tofu cannot start a turn yet"
	setupKeys      = "[1-9] run the fix   [r] check again   [q] quit"
	setupIndent    = "   "
	setupWatch     = "or run the command in another terminal: tofu picks it up here"
	setupPoll      = time.Second
	readyNote      = "type a task and press enter. tofu works in "
	gateOffLine    = "the gate is off, so no call on this session is judged."
	altPrefix      = "alt+"
	stoppingNote   = "stopping the turn"
	droppedQueue   = ", and the queue with it"
	toolEventKind  = "tool"
	failureHead    = "failure"
	partialHead    = "answer, interrupted"
	charactersKept = " characters were written and kept in work"
)

type App struct {
	options        Options
	requirements   []Requirement
	current        viewID
	strip          frame.Strip
	view           session.Model
	work           work.Model
	crew           crew.Model
	edits          edits.Model
	shells         shells.Model
	links          links.Model
	quote          quote.Model
	picker         models.Model
	settings       settings.Model
	settingsStore  *isettings.Store
	status         frame.Status
	wire           string
	model          string
	provider       string
	sessionName    string
	sessionID      string
	wires          []Wire
	width          int
	height         int
	started        time.Time
	busy           bool
	ticking        bool
	gateOff        bool
	cancel         context.CancelFunc
	events         chan Event
	board          paste.Board
	minted         int
	workBeforeTurn int
	selection      pick.Selection
	pressed        pick.Cell
	holding        bool
	frame          []string
}

type Closed struct{}

type requirementsMsg []Requirement

type recheckMsg struct{}

type pathsMsg []string

type tickMsg time.Time

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
	if options.Promotions == "" && options.Root != "" {
		options.Promotions = isession.NewPromotionLog(sys.StateDir(options.Root))
	}
	if options.Release == "" {
		options.Release = frame.Release(sys.Version(), sys.BuildRevision())
	}
	app := &App{
		options:       options,
		requirements:  options.Requirements,
		strip:         frame.Strip{Views: namedViews()},
		view:          session.New(options.Now, new(markdown.Renderer).Lines),
		work:          work.New(),
		edits:         edits.Model{Root: options.Root},
		settings:      settings.Model{Providers: options.Providers, Scopes: []string{"global", "project"}},
		settingsStore: options.Settings,
		width:         defaultWidth,
		height:        defaultHeight,
		started:       options.Now(),
		board:         paste.Default(options.Paste),
	}
	app.status.Note = options.Note
	app.view.Commands = commands(options)
	app.resize(app.width, app.height)
	app.readWires()
	app.refreshSettingsRows()
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
	signed := a.options.Wires()
	if len(signed) == 0 {
		return
	}
	a.wires = signed
	a.wire, a.model, a.provider = signed[0].Name, signed[0].Model, signed[0].Provider
}

func Run(options Options) error {
	_, err := tea.NewProgram(New(options)).Run()
	return err
}

func (a *App) Init() tea.Cmd {
	return tea.Batch(a.view.Focus(), a.pollQuota(), a.readPaths(), a.watchSetup(), a.pollShells())
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

func (a *App) resize(width, height int) {
	a.width, a.height = width, height
	a.view.SetSize(width, height-viewChrome)
	a.work.SetSize(width, height-viewChrome)
	a.crew.SetSize(width, height-viewChrome)
	a.edits.SetSize(width, height-viewChrome)
	a.shells.SetSize(width, height-viewChrome)
	a.links.SetSize(width, height-viewChrome)
	a.quote.SetSize(width, height-viewChrome)
	a.picker.SetSize(width, height-viewChrome)
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
		a.mousePress(msg)
		return a, nil

	case tea.MouseMotionMsg:
		a.mouseDrag(msg)
		return a, nil

	case tea.MouseReleaseMsg:
		return a, a.mouseRelease()

	case tea.MouseWheelMsg:
		a.mouseWheel(msg)
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

	case Closed:
		a.busy, a.cancel, a.events, a.edits.Busy = false, nil, nil, false
		a.view.Stop()
		a.dropSteering()
		next := tea.Batch(a.pollQuota(), a.readPaths(), a.pollShells())
		if task, queued := a.view.Release(); queued {
			return a, tea.Batch(a.start(task), next)
		}
		return a, next

	case pathsMsg:
		a.view.Paths = msg
		return a, nil

	case []frame.Quota:
		a.status.Quotas = msg
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

	case shellsMsg:
		a.shells.Set(msg)
		return a, nil
	}
	return a, a.view.Update(msg)
}

func jump(key string) (viewID, bool) {
	for index, view := range namedViews() {
		if key == string(view.Digit) {
			return viewID(index), true
		}
	}
	return viewChat, false
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
		a.stopTurn()
		return a, nil
	}
	if a.current == viewChat && a.view.TakesAnswerDigits() {
		switch key {
		case "1":
			a.answer(AllowedOnce)
			return a, nil
		case "2":
			a.answer(Denied)
			return a, nil
		case "3":
			a.answer(AlwaysHere)
			return a, nil
		}
	}
	if a.current == viewChat {
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
		a.show(viewChat)
		return a, nil
	case "ctrl+o":
		a.show(viewWork)
		return a, nil
	case "ctrl+v", "alt+v":
		if a.current != viewChat {
			return a, nil
		}
		return a, a.view.Paste(a.board)
	}
	digit, alt := strings.CutPrefix(key, altPrefix)
	if alt || a.current != viewChat {
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
	case viewShells:
		a.shellsKey(key)
		return a, nil
	case viewSettings:
		a.settingsKey(key)
		return a, nil
	case viewLinks:
		return a, a.linksKey(key)
	case viewQuote:
		a.quoteKey(key)
		return a, nil
	case viewModels:
		a.pickerKey(key)
		return a, nil
	case viewWork:
		a.workKey(key)
		return a, nil
	case viewChat:
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
	case "up":
		if a.view.HistoryUp() {
			return a, nil
		}
	case "down":
		if a.view.HistoryDown() {
			return a, nil
		}
	}
	if moved, scrolled := a.view.Scroll(key); scrolled {
		a.selection.Shift(moved)
		return a, nil
	}
	return a, a.view.Update(msg)
}

func (a *App) answer(answer Answer) {
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
	if prefix, isID := idPrefix(task); isID {
		a.view.Reset()
		a.jumpToID(prefix)
		return nil
	}
	chips := a.view.Remember(task)
	a.view.Reset()
	if a.busy {
		a.view.Queue(task, chips)
		a.steer(task)
		return nil
	}
	a.view.Append(session.Entry{Kind: session.User, Body: task, Chips: chips})
	return a.start(task)
}

func idPrefix(task string) (string, bool) {
	prefix, hasHash := strings.CutPrefix(task, "#")
	if !hasHash || prefix == "" {
		return "", false
	}
	for _, letter := range prefix {
		if !strings.ContainsRune("0123456789abcdefABCDEF", letter) {
			return "", false
		}
	}
	return strings.ToLower(prefix), true
}

func (a *App) jumpToID(prefix string) {
	if a.work.JumpTo(prefix) {
		a.show(viewWork)
		return
	}
	a.view.Append(session.Entry{Kind: session.Note, Body: "nothing in work carries the id #" + prefix})
}

func (a *App) workKey(key string) {
	a.work.Key(key)
	if key != "enter" {
		return
	}
	entry, picked := a.work.Picked()
	if !picked {
		return
	}
	freeArm := isession.PlaceChat
	if a.view.FoldedOutOfChat(entry.ID) {
		freeArm = isession.PlaceWork
	}
	a.view.Append(session.Entry{Kind: session.Note, ID: entry.ID, Body: entry.Reference()})
	a.recordPromotion(isession.Promotion{
		Action:    isession.ReachedIntoWork,
		EventID:   entry.ID,
		EventKind: entry.Kind(),
		FreeArm:   freeArm,
		Chose:     isession.PlaceChat,
	})
}

func (a *App) recordPromotion(row isession.Promotion) {
	row.At, row.Session = a.options.Now(), a.sessionID
	if err := a.options.Promotions.Append(row); err != nil {
		a.view.Append(session.Entry{Kind: session.Failure, Body: err.Error()})
	}
}

func (a *App) shellsKey(key string) {
	if key != "k" {
		a.shells.Key(key)
		return
	}
	entry, picked := a.shells.Picked()
	if !picked || a.options.KillShell == nil {
		return
	}
	if err := a.options.KillShell(entry.Name); err != nil {
		a.view.Append(session.Entry{Kind: session.Failure, Body: err.Error()})
		return
	}
	a.shells.Remove(entry.Name)
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

func (a *App) mintID() string {
	a.minted++
	return strconv.FormatInt(a.options.Now().UnixNano(), 16) + strconv.Itoa(a.minted)
}

func (a *App) turnWorkID() string {
	if len(a.work.Entries) <= a.workBeforeTurn {
		return ""
	}
	return a.work.Entries[len(a.work.Entries)-1].ID
}

func (a *App) stopTurn() {
	if a.view.Stopping {
		return
	}
	a.view.Stopping = true
	a.cancel()
	a.dropSteering()
	note := stoppingNote
	if a.view.DropQueue() {
		note = stoppingNote + droppedQueue
	}
	kept := a.keptPartial()
	a.view.Append(session.Entry{Kind: session.Note, Body: note})
	if kept != "" {
		a.view.Append(session.Entry{Kind: session.Note, Body: kept})
	}
}

func (a *App) keptPartial() string {
	partial, written := a.view.TakePartial()
	if !written {
		return ""
	}
	id := a.mintID()
	a.work.Append(work.Entry{ID: id, Head: partialHead, Output: partial, Bytes: len(partial)})
	return strconv.Itoa(len([]rune(partial))) + charactersKept + " [" + trace.Short(id) + "]"
}

func (a *App) start(task string) tea.Cmd {
	a.view.Follow()
	a.workBeforeTurn = len(a.work.Entries)
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
			return Closed{}
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
		a.view.Append(session.Entry{Kind: session.Assistant, Body: event.Text, ID: event.ID})
	case EventTextDelta:
		a.view.Stream(event.Text)
	case EventToolCall:
		a.view.Append(session.Entry{Kind: session.Tool, ID: event.ID, Head: event.Tool, Body: event.Text, Detail: event.Detail, Promoted: event.Promote})
		a.work.Append(work.Entry{ID: event.ID, Head: event.Tool + " " + event.Text, Args: event.Detail})
	case EventToolResult:
		status := event.Text
		if edit, changed := edits.Changed(event.Agent, a.view.Intent(event.ID), event.Diff, event.Created, event.ID, a.options.Now()); changed {
			a.edits.Add(edit)
			status = edit.Tally()
		}
		a.view.Finish(event.ID, session.Result{Status: status, Bytes: event.Bytes, Failed: event.Failed})
		a.work.Finish(event.ID, status, event.Bytes, event.Failed)
	case EventNote:
		a.view.Append(session.Entry{Kind: session.Note, Body: event.Text})
	case EventDone:
		a.view.Close(event.Text, a.turnWorkID())
	case EventFailure:
		id := cmp.Or(event.ID, a.mintID())
		a.work.Append(work.Entry{ID: id, Head: strings.TrimSpace(failureHead + " " + event.Tool), Output: event.Text, Failed: true})
		a.view.Append(session.Entry{Kind: session.Failure, ID: id, Body: event.Text})
	case EventDecision:
		if event.Decision != nil {
			a.view.Decide(*event.Decision)
			a.work.Decide(event.Decision.Tool, event.Decision.Verdict.String())
		}
	case EventSession:
		if event.ID != a.sessionID {
			a.started = a.options.Now()
		}
		a.sessionName, a.sessionID = event.Text, event.ID
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
		a.strip.Views[subAgentsIndex].Name = subAgentsLabel(a.status.Agents)
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
	poll := a.options.Quota
	if poll == nil {
		return nil
	}
	return func() tea.Msg { return poll() }
}

func (a *App) tick() tea.Cmd {
	if a.ticking || !a.busy {
		return nil
	}
	a.ticking = true
	return tea.Tick(session.TickInterval, func(at time.Time) tea.Msg { return tickMsg(at) })
}

func (a *App) settingsKey(key string) {
	a.applyIntent(a.settings.Key(key))
	a.refreshSettingsRows()
}

func (a *App) applyIntent(intent settings.Intent) {
	if a.settingsStore == nil {
		return
	}
	switch intent.Action {
	case settings.ActionNone:
	case settings.ActionCycleScope:
		a.settings.Scope = (a.settings.Scope + 1) % len(a.settings.Scopes)
	case settings.ActionToggle:
		wasOn := a.settingsStore.Bool(intent.Key)
		value := 1
		if wasOn {
			value = 0
		}
		a.setSetting(intent.Key, value)
		if intent.Key == isettings.ChatShowsTools {
			a.recordKindMove(wasOn)
		}
	case settings.ActionIncrement:
		a.setSetting(intent.Key, a.settingsStore.Int(intent.Key)+1)
	case settings.ActionDecrement:
		a.setSetting(intent.Key, a.settingsStore.Int(intent.Key)-1)
	default:
		panic("tui: unknown settings action")
	}
}

func (a *App) recordKindMove(wasInChat bool) {
	row := isession.Promotion{
		Action:    isession.MovedKind,
		EventKind: toolEventKind,
		FreeArm:   isession.PlaceWork,
		Chose:     isession.PlaceChat,
	}
	if wasInChat {
		row.FreeArm, row.Chose = isession.PlaceChat, isession.PlaceWork
	}
	a.recordPromotion(row)
}

func (a *App) setSetting(key string, value int) {
	if err := a.settingsStore.Set(isettings.Scope(a.settings.Scope), key, value); err != nil {
		a.view.Append(session.Entry{Kind: session.Failure, Body: err.Error()})
	}
}

func (a *App) refreshSettingsRows() {
	if a.settingsStore == nil {
		return
	}
	pending := map[string]bool{}
	for _, key := range a.settingsStore.RestartPending() {
		pending[key] = true
	}
	matches := a.settingsStore.Search(a.settings.Query)
	rows := make([]settings.Row, 0, len(matches))
	for _, match := range matches {
		rows = append(rows, settingsRow(a.settingsStore, pending, match.Spec))
	}
	a.settings.SetRows(rows)
	a.settings.ChatShowsTools = a.settingsStore.Bool(isettings.ChatShowsTools)
}

func settingsRow(store *isettings.Store, pending map[string]bool, spec isettings.Spec) settings.Row {
	scope, fromFile := store.Source(spec.Key)
	source := "default"
	if fromFile {
		source = scope.String() + " " + store.Path(scope)
	}
	value := strconv.Itoa(store.Int(spec.Key))
	if spec.Kind == isettings.Bool {
		value = strconv.FormatBool(store.Bool(spec.Key))
	}
	return settings.Row{
		Key:             spec.Key,
		Group:           spec.Group,
		Label:           spec.Label,
		Value:           value,
		Kind:            settings.Kind(spec.Kind),
		Changed:         fromFile,
		RestartRequired: spec.Restart,
		RestartPending:  pending[spec.Key],
		Source:          source,
	}
}

func (a *App) View() tea.View {
	at := a.options.Now()
	head := frame.Head{
		Path:        a.options.Repo,
		Branch:      a.options.Branch,
		Provider:    a.provider,
		Model:       a.model,
		SessionName: a.sessionName,
		SessionID:   a.sessionID,
		At:          at,
		Started:     a.started,
	}
	rows := []string{frame.Header(head, a.width)}
	var caret *tea.Cursor
	if len(a.requirements) > 0 {
		rows = append(rows, a.setupView(a.height-headerRows))
	} else {
		status := a.status
		status.At, status.Release = at, a.options.Release
		a.view.ChatShowsTools = a.settings.ChatShowsTools
		if a.settingsStore != nil {
			a.view.FoldHidesShell = a.settingsStore.Bool(isettings.FoldHidesShell)
		}
		rows = append(rows, a.strip.Render(a.width), a.body())
		if a.current != viewCrew {
			rows = append(rows, frame.Bar(status, a.width))
		}
		if a.current == viewChat {
			caret = a.view.Cursor()
		}
	}
	if caret != nil {
		caret.Y += bodyRow
	}
	a.frame = strings.Split(lipgloss.JoinVertical(lipgloss.Left, rows...), "\n")
	view := tea.NewView(strings.Join(a.selection.Paint(a.frame), "\n"))
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	view.Cursor = caret
	return view
}

func (a *App) body() string {
	switch a.current {
	case viewChat:
		return a.view.View()
	case viewWork:
		return a.work.View()
	case viewCrew:
		return a.crew.View()
	case viewEdits:
		return a.edits.View()
	case viewShells:
		return a.shells.View()
	case viewSettings:
		return a.settings.View()
	case viewLinks:
		return a.links.View()
	case viewQuote:
		return a.quote.View()
	case viewModels:
		return a.picker.View()
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
