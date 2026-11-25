package host

import (
	"cmp"
	"maps"
	"slices"
	"strconv"
	"strings"

	"tofu/internal/agent/loop"
	roster "tofu/internal/agent/subagents"
	"tofu/internal/tools/shell/process"
	"tofu/internal/ui/statusline"
)

const (
	statusApp    = "tofu"
	statusMethod = "status"
)

type StatusReport struct {
	Identity
	statusline.Record
}

type StatusList struct {
	Records []StatusRecord `json:"records"`
}

type StatusRecord struct {
	Session string `json:"session"`
	statusline.Record
}

type statusFeed struct {
	board   statusline.Board
	lead    statusline.State
	failure string
	failed  string
	cron    string
	asks    []Event
	agents  []SubAgentRow
	acked   map[string]bool
}

type StatusListParams struct {
	Session string `json:"session,omitempty"`
}

type StatusAckParams struct {
	Session string `json:"session,omitempty"`
	ID      string `json:"id"`
}

type shellStatus struct {
	program statusline.Board
	stream  statusline.Stream
	ranHere bool
}

func (f *statusFeed) follow(event Event) {
	switch event.Kind {
	case EventTurnStarted:
		f.lead, f.failure, f.cron = statusline.Working, "", ""
		if event.Origin.Kind == OriginCron {
			f.cron = event.Origin.Job
		}
	case EventAwaitPerson:
		f.asks = append(f.asks, event)
	case EventResumed:
		f.asks = slices.DeleteFunc(f.asks, func(asked Event) bool { return asked.ID == event.ID })
	case EventFailure:
		if event.Agent == "" {
			f.failure = event.Text
		}
	case EventDone:
		if event.Status == StatusFailed && f.failure == "" {
			f.failure = event.Text
		}
		if event.Text == cancelledAt {
			f.lead = statusline.Idle
		}
		if event.SubAgents != nil {
			f.agents = event.SubAgents
		}
	case EventSubAgent:
		f.agents = event.SubAgents
	case EventTurnEnded:
		f.asks = slices.DeleteFunc(f.asks, func(asked Event) bool { return asked.Questions == nil })
		switch {
		case f.failure != "":
			f.lead = statusline.Errored
		case f.lead != statusline.Idle:
			f.lead = statusline.Done
		}
		f.failed, f.failure = f.failure, ""
	}
}

func parentsOf(rows []SubAgentRow) map[string]string {
	parentOf := make(map[string]string, len(rows))
	for _, row := range rows {
		if _, held := parentOf[row.Name]; !held {
			parentOf[row.Name] = row.Parent
		}
	}
	return parentOf
}

func agentStatusID(parentOf map[string]string, name string) string {
	parts := []string{"agents", name}
	for range parentOf {
		parent, held := parentOf[name]
		if !held || parent == "" || slices.Contains(parts, parent) {
			break
		}
		name = parent
		parts = append([]string{"agents", name}, parts...)
	}
	return statusline.Path(parts...)
}

func AgentStatuses(rows []SubAgentRow) []statusline.Record {
	parentOf := parentsOf(rows)
	records := make([]statusline.Record, len(rows))
	for at, row := range rows {
		records[at] = agentStatus(row, parentOf)
	}
	return records
}

func agentStatus(row SubAgentRow, parentOf map[string]string) statusline.Record {
	record := statusline.Record{ID: agentStatusID(parentOf, row.Name), Title: row.Name, Msg: row.Doing}
	switch row.State {
	case roster.Working, roster.Reopened:
		record.State = statusline.Working
	case roster.WaitingAnswer:
		record.State, record.Kind = statusline.Blocked, statusline.Question
	case roster.InReview, roster.Finished:
		record.State = statusline.Done
	case roster.Parked:
		record.State = statusline.Idle
	case roster.Errored:
		record.State = statusline.Errored
	default:
		panic("host: unknown sub-agent state " + row.State.String())
	}
	return record
}

func AskStatus(ask Event) (statusline.Kind, string) {
	if ask.Tool == loop.AskPersonToolName {
		return statusline.Question, ask.Text
	}
	return statusline.Permission, ask.Tool + ": " + ask.Text
}

func (f *statusFeed) records(shells map[string]*watchedShell) []statusline.Record {
	lead := statusline.Record{ID: statusApp, State: statusline.Idle, App: statusApp}
	if f.lead != "" {
		lead.State = f.lead
	}
	if lead.State == statusline.Errored {
		lead.Msg = f.failed
	}
	asked := map[string]Event{}
	for _, ask := range f.asks {
		asked[ask.Agent] = ask
	}
	if ask, blocked := asked[""]; blocked {
		lead.State, lead.Ask = statusline.Blocked, ask.ID
		lead.Kind, lead.Msg = AskStatus(ask)
	}
	records := []statusline.Record{lead}
	if f.cron != "" {
		records = append(records, statusline.Record{ID: statusline.Path("cron", f.cron), State: lead.State, Kind: lead.Kind, App: statusApp, Title: f.cron, Msg: lead.Msg, Ask: lead.Ask})
	}
	for at, record := range AgentStatuses(f.agents) {
		row := f.agents[at]
		if ask, blocked := asked[row.Name]; blocked {
			record.State, record.Ask = statusline.Blocked, ask.ID
			record.Kind, record.Msg = AskStatus(ask)
		}
		record.App = statusApp
		records = append(records, record)
	}
	for _, name := range slices.Sorted(maps.Keys(shells)) {
		records = append(records, shells[name].records(name)...)
	}
	return records
}

func (w *watchedShell) records(name string) []statusline.Record {
	if !w.status.ranHere || w.state == process.Killed {
		return nil
	}
	id := statusline.Path("shells", name)
	own := statusline.Record{ID: id, State: statusline.Working, App: statusApp, Title: name, Msg: w.command}
	running := w.state == process.Running
	if !running {
		own.State = statusline.Done
		if w.exitCode != nil && *w.exitCode != 0 {
			own.State, own.Msg = statusline.Errored, "exit "+strconv.Itoa(*w.exitCode)+": "+w.command
		}
	}
	held := running && w.holder == process.HolderPerson
	var records []statusline.Record
	for _, reported := range w.status.program.List() {
		lasting := reported.State == statusline.Done || reported.State == statusline.Errored
		if !running && !lasting {
			continue
		}
		if held && reported.State == statusline.Blocked {
			reported.State, reported.Ask = statusline.Working, ""
		}
		if reported.ID == "" {
			reported.ID, reported.Title = id, cmp.Or(reported.Title, name)
			own = reported
			continue
		}
		if nested := id + "/" + reported.ID; statusline.Path(strings.Split(nested, "/")...) == nested {
			reported.ID = nested
			records = append(records, reported)
		}
	}
	if held {
		own.Msg = "held by you: " + w.command
	}
	return append([]statusline.Record{own}, records...)
}

func (f *statusFeed) unacked(records []statusline.Record) []statusline.Record {
	return slices.DeleteFunc(records, func(record statusline.Record) bool {
		if !record.State.Finished() {
			delete(f.acked, record.ID)
		}
		return f.acked[record.ID]
	})
}

func (s *server) reportStatus(l *lane) {
	var shells map[string]*watchedShell
	if l == s.focus() {
		shells = s.shells
	}
	s.sendStatus(l, l.status.board.Sync(l.status.unacked(l.status.records(shells))))
}

func (s *server) sendStatus(l *lane, records []statusline.Record) {
	for _, record := range records {
		agent := ""
		if strings.HasPrefix(record.ID, "agents/") {
			agent = record.Title
		}
		s.box.push(kept(statusMethod, &StatusReport{Identity: l.items.identity(agent, record.ID), Record: record}))
	}
}

func (s *server) statusList(p StatusListParams) (any, error) {
	lanes := s.tracked()
	if p.Session != "" {
		l, err := s.lane(p.Session)
		if err != nil {
			return nil, err
		}
		lanes = []*lane{l}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	records := []StatusRecord{}
	for _, l := range lanes {
		for _, record := range l.status.board.List() {
			records = append(records, StatusRecord{Session: l.items.session, Record: record})
		}
	}
	return StatusList{Records: records}, nil
}

func (s *server) statusAck(p StatusAckParams) (any, error) {
	l, err := s.lane(p.Session)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	listed := l.status.board.List()
	at := slices.IndexFunc(listed, func(record statusline.Record) bool { return record.ID == p.ID })
	switch {
	case at < 0:
		return nil, &Refusal{Code: CodeRefused, Message: "no status record is listed as " + strconv.Quote(p.ID)}
	case !listed[at].State.Finished():
		return nil, &Refusal{Code: CodeRefused, Message: "status record " + strconv.Quote(p.ID) + " is " + string(listed[at].State) + ": only a done or error record is acknowledged"}
	}
	l.status.acked[p.ID] = true
	s.reportStatus(l)
	return Ack{OK: true}, nil
}
