package tui

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"boji/interface/tui/frame"
	"boji/interface/tui/session"
	"boji/interface/tui/theme"
	"boji/interface/tui/widget"
)

type EventKind int

const (
	EventText EventKind = iota
	EventToolCall
	EventToolResult
	EventNote
	EventFailure
	EventStats
	EventDone
)

type Event struct {
	Kind      EventKind
	Tool      string
	Text      string
	Model     string
	TokensIn  int
	TokensOut int
	Decisions int
}

type Turn func(ctx context.Context, task string, emit func(Event))

type Requirement struct {
	What string
	Fix  string
}

type Options struct {
	Repo         string
	Branch       string
	Model        string
	Note         string
	Requirements []Requirement
	Recheck      func() []Requirement
	Login        func() *exec.Cmd
	Quota        func() frame.Quota
	Turn         Turn
	Now          func() time.Time
}

const (
	eventBuffer   = 256
	tickInterval  = time.Second
	defaultWidth  = 80
	defaultHeight = 24
	setupTitle    = "boji cannot start a turn yet"
	setupKeys     = "[l] run the login   [r] check again   [q] quit"
	setupIndent   = "   "
)

type App struct {
	options      Options
	requirements []Requirement
	view         session.Model
	status       frame.Status
	model        string
	width        int
	height       int
	started      time.Time
	busy         bool
	cancel       context.CancelFunc
	events       chan Event
}

type closedMsg struct{}

type requirementsMsg []Requirement

type tickMsg time.Time

func New(options Options) *App {
	if options.Now == nil {
		options.Now = time.Now
	}
	app := &App{
		options:      options,
		requirements: options.Requirements,
		view:         session.New(),
		model:        options.Model,
		width:        defaultWidth,
		height:       defaultHeight,
		started:      options.Now(),
	}
	app.status.Note = options.Note
	app.view.SetSize(app.width, app.height-2)
	if len(app.requirements) == 0 {
		app.view.Append(session.Entry{Kind: session.Note, Body: "type a task and press enter. boji works in " + options.Repo})
	}
	return app
}

func Run(options Options) error {
	_, err := tea.NewProgram(New(options)).Run()
	return err
}

func (a *App) Init() tea.Cmd { return tea.Batch(a.view.Focus(), a.pollQuota()) }

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
		a.view.SetSize(a.width, a.height-2)
		return a, nil

	case tea.KeyPressMsg:
		return a.key(msg)

	case Event:
		a.absorb(msg)
		if a.events == nil {
			return a, nil
		}
		return a, a.waitForEvent()

	case closedMsg:
		a.busy, a.cancel, a.events = false, nil, nil
		a.view.Busy = false
		return a, a.pollQuota()

	case frame.Quota:
		a.status.Quota = msg
		return a, nil

	case requirementsMsg:
		a.requirements = msg
		return a, nil

	case tickMsg:
		if !a.busy {
			return a, nil
		}
		return a, tick()
	}
	return a, a.view.Update(msg)
}

func (a *App) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if len(a.requirements) > 0 {
		return a.setupKey(key)
	}
	switch key {
	case "ctrl+c":
		if a.busy {
			a.cancel()
			a.view.Append(session.Entry{Kind: session.Note, Body: "stopping the turn"})
			return a, nil
		}
		return a, tea.Quit
	case "enter":
		return a, a.send()
	}
	return a, a.view.Update(msg)
}

func (a *App) setupKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q", "ctrl+c":
		return a, tea.Quit
	case "r":
		return a, func() tea.Msg { return a.checkedRequirements() }
	case "l":
		if a.options.Login == nil {
			return a, nil
		}
		return a, tea.ExecProcess(a.options.Login(), func(error) tea.Msg { return a.checkedRequirements() })
	}
	return a, nil
}

func (a *App) send() tea.Cmd {
	task := a.view.Value()
	if task == "" || a.busy {
		return nil
	}
	a.view.Reset()
	a.view.Append(session.Entry{Kind: session.User, Body: task})
	if a.options.Turn == nil {
		a.view.Append(session.Entry{Kind: session.Failure, Body: "no engine is wired to this app"})
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan Event, eventBuffer)
	a.busy, a.cancel, a.events = true, cancel, events
	a.view.Busy = true
	turn := a.options.Turn
	go func() {
		turn(ctx, task, func(event Event) { events <- event })
		cancel()
		close(events)
	}()
	return tea.Batch(a.waitForEvent(), tick())
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
	switch event.Kind {
	case EventText:
		a.view.Append(session.Entry{Kind: session.Assistant, Body: event.Text})
	case EventToolCall:
		a.view.Append(session.Entry{Kind: session.Tool, Head: event.Tool, Body: event.Text})
	case EventToolResult:
		a.view.Append(session.Entry{Kind: session.Result, Body: event.Text})
	case EventNote, EventDone:
		a.view.Append(session.Entry{Kind: session.Note, Body: event.Text})
	case EventFailure:
		a.view.Append(session.Entry{Kind: session.Failure, Body: event.Text})
	case EventStats:
		a.status.TokensIn, a.status.TokensOut = event.TokensIn, event.TokensOut
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

func tick() tea.Cmd {
	return tea.Tick(tickInterval, func(at time.Time) tea.Msg { return tickMsg(at) })
}

func (a *App) View() tea.View {
	at := a.options.Now()
	head := frame.Head{
		Repo:    a.options.Repo,
		Branch:  a.options.Branch,
		Model:   a.model,
		At:      at,
		Elapsed: at.Sub(a.started),
	}
	status := a.status
	status.At = at
	body := a.view.View()
	if len(a.requirements) > 0 {
		body = a.setupView()
	}
	view := tea.NewView(lipgloss.JoinVertical(lipgloss.Left,
		frame.Header(head, a.width),
		body,
		frame.Bar(status, a.width),
	))
	view.AltScreen = true
	return view
}

func (a *App) setupView() string {
	lines := []string{theme.Warn().Render(widget.Fit(setupTitle, a.width)), ""}
	for index, requirement := range a.requirements {
		for _, line := range widget.Wrap(strconv.Itoa(index+1)+". "+requirement.What, a.width) {
			lines = append(lines, theme.Text().Render(line))
		}
		for _, line := range widget.Wrap(requirement.Fix, a.width-len(setupIndent)) {
			lines = append(lines, theme.Dim().Render(setupIndent+line))
		}
		lines = append(lines, "")
	}
	lines = append(lines, theme.Faint().Render(widget.Fit(setupKeys, a.width)))
	for len(lines) < a.height-2 {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}
