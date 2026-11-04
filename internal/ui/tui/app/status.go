package app

import (
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"tofu/internal/host"
	"tofu/internal/ui/statusline"
	"tofu/internal/ui/tui/panels/shells"
)

const statusApp = "tofu"

type focus int

const (
	focusUnknown focus = iota
	focusIn
	focusOut
)

type statusWatch struct {
	shown   statusline.Board
	after   statusline.State
	failure string
	failed  string
	cron    string
	focus   focus
	asks    []Event
	seen    map[string]time.Time
	rows    []host.SubAgentRow
	agents  []statusline.Record
	records []statusline.Record
}

func (w *statusWatch) askOf(agent string) (Event, bool) {
	for _, ask := range slices.Backward(w.asks) {
		if ask.Agent == agent {
			return ask, true
		}
	}
	return Event{}, false
}

func sameForStatus(a, b host.SubAgentRow) bool {
	return a.Name == b.Name && a.Parent == b.Parent && a.Doing == b.Doing && a.State == b.State
}

func (a *App) agentRecords() []statusline.Record {
	watch := &a.statusWatch
	if !slices.EqualFunc(watch.rows, a.subAgents, sameForStatus) {
		watch.rows, watch.agents = slices.Clone(a.subAgents), make([]statusline.Record, len(a.subAgents))
		for index, row := range a.subAgents {
			watch.agents[index] = host.AgentStatus(row, a.subAgents)
		}
	}
	return watch.agents
}

func (a *App) followStatus(msg tea.Msg) {
	watch := &a.statusWatch
	switch msg := msg.(type) {
	case tea.FocusMsg:
		watch.focus = focusIn
		a.leadSeen()
	case tea.BlurMsg:
		watch.focus = focusOut
	case tea.KeyPressMsg, tea.MouseClickMsg:
		a.leadSeen()
	case Event:
		switch {
		case msg.Kind == EventAwaitPerson:
			watch.asks = append(watch.asks, msg)
		case msg.Kind == EventResumed:
			watch.asks = slices.DeleteFunc(watch.asks, func(asked Event) bool { return asked.ID == msg.ID })
		case msg.Kind == EventFailure && msg.Agent == "":
			watch.failure = msg.Text
		case msg.Kind == EventDone && msg.Status == host.StatusFailed:
			watch.failure = msg.Text
		case msg.Kind == EventTask && msg.Origin.Kind == host.OriginCron:
			watch.cron = msg.Origin.Job
		}
	case Closed:
		watch.after = statusline.Done
		watch.asks = slices.DeleteFunc(watch.asks, func(asked Event) bool { return asked.Questions == nil })
		switch {
		case watch.failure != "":
			watch.after = statusline.Errored
		case a.view.Stopping || watch.focus == focusIn:
			watch.after = statusline.Idle
		}
		watch.failed, watch.failure = watch.failure, ""
	}
}

func (a *App) leadSeen() {
	if !a.busy {
		a.statusWatch.after, a.statusWatch.cron = statusline.Idle, ""
	}
}

func (a *App) reportStatus() tea.Cmd {
	var reports strings.Builder
	for _, record := range a.statusWatch.shown.Sync(a.statusRecords()) {
		reports.WriteString(statusline.Encode(record))
	}
	if reports.Len() == 0 {
		return nil
	}
	return tea.Raw(reports.String())
}

func (a *App) statusRecords() []statusline.Record {
	watch := &a.statusWatch
	lead := statusline.Record{State: statusline.Idle, App: statusApp, Title: a.sessionName}
	if a.busy {
		lead.State = statusline.Working
	} else if watch.after != "" {
		lead.State = watch.after
	}
	if lead.State == statusline.Errored {
		lead.Msg = watch.failed
	}
	if ask, blocked := watch.askOf(""); blocked {
		lead.State = statusline.Blocked
		lead.Kind, lead.Msg = host.AskStatus(ask)
	}
	records := watch.records[:0]
	records = append(records, lead)
	if watch.cron != "" {
		records = append(records, statusline.Record{ID: statusline.Path("cron", watch.cron), State: lead.State, Kind: lead.Kind, Title: watch.cron, Msg: lead.Msg})
	}
	agents := a.agentRecords()
	for index, row := range a.subAgents {
		record := agents[index]
		if ask, blocked := watch.askOf(row.Name); blocked {
			record.State = statusline.Blocked
			record.Kind, record.Msg = host.AskStatus(ask)
		}
		if a.unseen(record, row.Ended, a.current == screenAgents) {
			records = append(records, record)
		}
	}
	for _, entry := range slices.Backward(a.shells.Entries) {
		record, ended := shellRecord(entry)
		if record.State != statusline.Clear && a.unseen(record, ended, a.current == screenShells) {
			records = append(records, record)
		}
	}
	watch.records = records
	return records
}

func (a *App) unseen(record statusline.Record, ended time.Time, viewed bool) bool {
	if record.State != statusline.Done && record.State != statusline.Errored {
		return true
	}
	if a.statusWatch.seen == nil {
		a.statusWatch.seen = map[string]time.Time{}
	}
	if viewed {
		a.statusWatch.seen[record.ID] = ended
	}
	return !a.statusWatch.seen[record.ID].Equal(ended)
}

func shellRecord(entry shells.Entry) (statusline.Record, time.Time) {
	record := statusline.Record{ID: statusline.Path("shells", entry.Name), Title: entry.Name, Msg: entry.Command}
	var ended time.Time
	if entry.Ended != nil {
		ended = *entry.Ended
	}
	switch entry.State {
	case shells.Running, shells.LeftOver:
		record.State = statusline.Working
	case shells.Exited:
		record.State = statusline.Done
		if entry.ExitCode != nil && *entry.ExitCode != 0 {
			record.State, record.Msg = statusline.Errored, "exit "+strconv.Itoa(*entry.ExitCode)+": "+entry.Command
		}
	case shells.Killed:
		record.State = statusline.Clear
	default:
		panic("tui: unknown shell state " + string(entry.State))
	}
	return record, ended
}
