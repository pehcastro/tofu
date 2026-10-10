package host

import (
	"cmp"
	"maps"
	"slices"
	"strconv"
	"strings"

	"tofu/internal/shell"
	"tofu/internal/status"
	roster "tofu/internal/subagent"
	"tofu/internal/turn"
)

const (
	statusApp    = "tofu"
	statusMethod = "status"
)

type StatusReport struct {
	Identity
	status.Record
}

type StatusList struct {
	Records []StatusRecord `json:"records"`
}

type StatusRecord struct {
	Session string `json:"session"`
	status.Record
}

type statusFeed struct {
	board   status.Board
	lead    status.State
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
	program status.Board
	stream  status.Stream
	ranHere bool
}

func (f *statusFeed) follow(event Event) {
	switch event.Kind {
	case EventTurnStarted:
		f.lead, f.failure, f.cron = status.Working, "", ""
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
			f.lead = status.Idle
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
			f.lead = status.Errored
		case f.lead != status.Idle:
			f.lead = status.Done
		}
		f.failed, f.failure = f.failure, ""
	}
}

func agentStatusID(rows []SubAgentRow, name string) string {
	parts := []string{"agents", name}
	for range rows {
		at := slices.IndexFunc(rows, func(row SubAgentRow) bool { return row.Name == name })
		if at < 0 || rows[at].Parent == "" || slices.Contains(parts, rows[at].Parent) {
			break
		}
		name = rows[at].Parent
		parts = append([]string{"agents", name}, parts...)
	}
	return status.Path(parts...)
}

func AgentStatus(row SubAgentRow, rows []SubAgentRow) status.Record {
	record := status.Record{ID: agentStatusID(rows, row.Name), Title: row.Name, Msg: row.Doing}
	switch row.State {
	case roster.Working, roster.Reopened:
		record.State = status.Working
	case roster.WaitingAnswer:
		record.State, record.Kind = status.Blocked, status.Question
	case roster.InReview, roster.Finished:
		record.State = status.Done
	case roster.Parked:
		record.State = status.Idle
	case roster.Errored:
		record.State = status.Errored
	default:
		panic("host: unknown sub-agent state " + row.State.String())
	}
	return record
}

func AskStatus(ask Event) (status.Kind, string) {
	if ask.Tool == turn.AskPersonToolName {
		return status.Question, ask.Text
	}
	return status.Permission, ask.Tool + ": " + ask.Text
}

func (f *statusFeed) records(shells map[string]*watchedShell) []status.Record {
	lead := status.Record{ID: statusApp, State: status.Idle, App: statusApp}
	if f.lead != "" {
		lead.State = f.lead
	}
	if lead.State == status.Errored {
		lead.Msg = f.failed
	}
	asked := map[string]Event{}
	for _, ask := range f.asks {
		asked[ask.Agent] = ask
	}
	if ask, blocked := asked[""]; blocked {
		lead.State, lead.Ask = status.Blocked, ask.ID
		lead.Kind, lead.Msg = AskStatus(ask)
	}
	records := []status.Record{lead}
	if f.cron != "" {
		records = append(records, status.Record{ID: status.Path("cron", f.cron), State: lead.State, Kind: lead.Kind, App: statusApp, Title: f.cron, Msg: lead.Msg, Ask: lead.Ask})
	}
	for _, row := range f.agents {
		record := AgentStatus(row, f.agents)
		if ask, blocked := asked[row.Name]; blocked {
			record.State, record.Ask = status.Blocked, ask.ID
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

func (w *watchedShell) records(name string) []status.Record {
	if !w.status.ranHere || w.state == shell.Killed {
		return nil
	}
	id := status.Path("shells", name)
	own := status.Record{ID: id, State: status.Working, App: statusApp, Title: name, Msg: w.command}
	running := w.state == shell.Running
	if !running {
		own.State = status.Done
		if w.exitCode != nil && *w.exitCode != 0 {
			own.State, own.Msg = status.Errored, "exit "+strconv.Itoa(*w.exitCode)+": "+w.command
		}
	}
	var records []status.Record
	for _, reported := range w.status.program.List() {
		lasting := reported.State == status.Done || reported.State == status.Errored
		if !running && !lasting {
			continue
		}
		if reported.ID == "" {
			reported.ID, reported.Title = id, cmp.Or(reported.Title, name)
			own = reported
			continue
		}
		if nested := id + "/" + reported.ID; status.Path(strings.Split(nested, "/")...) == nested {
			reported.ID = nested
			records = append(records, reported)
		}
	}
	return append([]status.Record{own}, records...)
}

func (f *statusFeed) unacked(records []status.Record) []status.Record {
	return slices.DeleteFunc(records, func(record status.Record) bool {
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

func (s *server) sendStatus(l *lane, records []status.Record) {
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
	at := slices.IndexFunc(listed, func(record status.Record) bool { return record.ID == p.ID })
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
