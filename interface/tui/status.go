package tui

import (
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"tofu/interface/tui/shells"
	"tofu/internal/host"
	"tofu/internal/status"
)

const statusApp = "tofu"

type focus int

const (
	focusUnknown focus = iota
	focusIn
	focusOut
)

type statusWatch struct {
	shown   status.Board
	after   status.State
	failure string
	failed  string
	cron    string
	focus   focus
	asks    []Event
	seen    map[string]time.Time
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
		watch.after = status.Done
		watch.asks = slices.DeleteFunc(watch.asks, func(asked Event) bool { return asked.Questions == nil })
		switch {
		case watch.failure != "":
			watch.after = status.Errored
		case a.view.Stopping || watch.focus == focusIn:
			watch.after = status.Idle
		}
		watch.failed, watch.failure = watch.failure, ""
	}
}

func (a *App) leadSeen() {
	if !a.busy {
		a.statusWatch.after, a.statusWatch.cron = status.Idle, ""
	}
}

func (a *App) reportStatus() tea.Cmd {
	var reports strings.Builder
	for _, record := range a.statusWatch.shown.Sync(a.statusRecords()) {
		reports.WriteString(status.Encode(record))
	}
	if reports.Len() == 0 {
		return nil
	}
	return tea.Raw(reports.String())
}

func (a *App) statusRecords() []status.Record {
	watch := &a.statusWatch
	lead := status.Record{State: status.Idle, App: statusApp, Title: a.sessionName}
	if a.busy {
		lead.State = status.Working
	} else if watch.after != "" {
		lead.State = watch.after
	}
	if lead.State == status.Errored {
		lead.Msg = watch.failed
	}
	asked := map[string]Event{}
	for _, ask := range watch.asks {
		asked[ask.Agent] = ask
	}
	if ask, blocked := asked[""]; blocked {
		lead.State = status.Blocked
		lead.Kind, lead.Msg = host.AskStatus(ask)
	}
	records := []status.Record{lead}
	if watch.cron != "" {
		records = append(records, status.Record{ID: status.Path("cron", watch.cron), State: lead.State, Kind: lead.Kind, Title: watch.cron, Msg: lead.Msg})
	}
	for _, row := range a.subAgents {
		record := host.AgentStatus(row, a.subAgents)
		if ask, blocked := asked[row.Name]; blocked {
			record.State = status.Blocked
			record.Kind, record.Msg = host.AskStatus(ask)
		}
		if a.unseen(record, row.Ended, a.current == screenAgents) {
			records = append(records, record)
		}
	}
	for _, entry := range slices.Backward(a.shells.Entries) {
		record, ended := shellRecord(entry)
		if record.State != status.Clear && a.unseen(record, ended, a.current == screenShells) {
			records = append(records, record)
		}
	}
	return records
}

func (a *App) unseen(record status.Record, ended time.Time, viewed bool) bool {
	if record.State != status.Done && record.State != status.Errored {
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

func shellRecord(entry shells.Entry) (status.Record, time.Time) {
	record := status.Record{ID: status.Path("shells", entry.Name), Title: entry.Name, Msg: entry.Command}
	var ended time.Time
	if entry.Ended != nil {
		ended = *entry.Ended
	}
	switch entry.State {
	case shells.Running, shells.LeftOver:
		record.State = status.Working
	case shells.Exited:
		record.State = status.Done
		if entry.ExitCode != nil && *entry.ExitCode != 0 {
			record.State, record.Msg = status.Errored, "exit "+strconv.Itoa(*entry.ExitCode)+": "+entry.Command
		}
	case shells.Killed:
		record.State = status.Clear
	default:
		panic("tui: unknown shell state " + string(entry.State))
	}
	return record, ended
}
