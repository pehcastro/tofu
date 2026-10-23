package host

import (
	"cmp"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"tofu/internal/llm"
	"tofu/internal/session"
	roster "tofu/internal/subagent"
	"tofu/internal/sys"
	"tofu/internal/turn"
)

type recordedCall struct {
	call, result session.Event
	spawn        SubAgentRow
}

type recordedReport struct {
	agent string
	state roster.State
	text  string
	drawn bool
}

type resumedLead struct {
	watch   *watcher
	calls   map[string]*recordedCall
	reports []recordedReport
	rows    []SubAgentRow
	said    []recordedMessage
	loose   []session.Attachment
	images  map[string][]session.Attachment
	runs    map[string]session.AgentRun
	worked  map[string][]session.Event
	turn    string
	at      time.Time
}

type recordedMessage struct {
	logged session.Event
	body   session.MessageBody
	taken  bool
}

func resumedChat(carry Carry, dir string) []Event {
	if carry.Session == "" {
		return nil
	}
	var chat []Event
	keys := carry.Keys
	if keys == nil {
		loaded := sys.LoadKeyRedactor()
		keys = &loaded
	}
	mask := keys.Redact
	lead := &resumedLead{calls: map[string]*recordedCall{}, worked: map[string][]session.Event{}, runs: map[string]session.AgentRun{}, images: map[string][]session.Attachment{}}
	lead.watch = &watcher{emit: func(event Event) {
		event.Turn, event.At = lead.turn, lead.at
		chat = append(chat, redacted(event, mask))
	}, turnID: carry.Session, seen: map[string]bool{}, spawner: &turn.SpawnTool{}}
	labelled := Event{Kind: EventSession, Text: carry.Name, ID: carry.Session, Root: carry.Session}
	store, err := carry.reading(dir)
	if err == nil {
		lead.read(store, carry.Session)
		labelled = labelledAs(store, labelled)
		store.ForgetRead()
	}
	lead.watch.emit(labelled)
	for _, message := range carry.Messages {
		if message.Origin.Source == llm.SourceSpawnLine {
			continue
		}
		said := lead.recorded(message)
		if said != nil {
			lead.at = said.At
		}
		if said != nil && message.Role == llm.RoleUser && (message.Origin.Source == llm.SourceTask || message.Origin.Source == llm.SourceCronFire) {
			lead.turn, lead.watch.turnID = said.Turn, cmp.Or(said.Turn, carry.Session)
		}
		switch message.Role {
		case llm.RoleUser:
			if endsTheRow(message.Origin.Source) {
				lead.watch.endLeadRow()
			}
			task, origin := carry.taskIn(message.Content), lead.originOf(message)
			left, drew := lead.reportsIn(task)
			switch {
			case origin.Sub == llm.SourceEnvironment:
			case !drew || origin.Sub == llm.SourceReport || origin.Sub == llm.SourceReportFor:
				var images []MessageImage
				if said != nil && store != nil {
					images = shownAttached(task, lead.images[said.ID], store)
				}
				lead.watch.emit(Event{Kind: EventTask, Text: task, Origin: origin, Images: images})
			case left != "" && origin.Kind == OriginPerson:
				lead.watch.emit(Event{Kind: EventTask, Text: left, Origin: origin})
			}
		case llm.RoleAssistant:
			if text := strings.TrimSpace(message.Content); text != "" {
				lead.watch.endLeadRow()
				lead.watch.emit(Event{Kind: EventText, Text: text})
			}
			for _, call := range message.ToolCalls {
				recorded := lead.calls[call.ID]
				if recorded != nil && recorded.call.Call != "" {
					lead.at = recorded.call.At
				}
				lead.watch.called(call, "")
				if recorded != nil && recorded.spawn.Name != "" && !lead.drawn(recorded.spawn.Name) {
					lead.spawned(recorded.spawn)
				}
			}
		case llm.RoleTool:
			if recorded := lead.calls[message.ToolCallID]; recorded != nil && recorded.result.Call != "" {
				ran := resultOf(recorded.result)
				message.ToolExitCode, message.ToolHits, lead.at = ran.ToolExitCode, ran.ToolHits, recorded.result.At
			}
			lead.watch.result(message, "")
		case llm.RoleSystem, llm.RoleUnknown:
		}
		if said != nil {
			lead.watch.emit(Event{Kind: EventPersisted, ID: said.ID, Logged: said})
		}
	}
	lead.settleTheRunning(store, carry.Session)
	return chat
}

func (l *resumedLead) read(store *session.Store, id string) {
	lineage, _ := store.Lineage(id)
	for _, generation := range lineage {
		for _, event := range generation.Events {
			l.note(event)
		}
		for _, run := range generation.Header.Agents {
			l.runs[run.Agent] = run
		}
	}
}

func (l *resumedLead) note(event session.Event) {
	if event.Kind == session.EventReport {
		var report session.ReportBody
		_ = json.Unmarshal(event.Body, &report)
		if at := slices.IndexFunc(roster.States(), func(state roster.State) bool { return state.String() == report.State }); at >= 0 && report.Text != "" {
			l.reports = append(l.reports, recordedReport{agent: event.Agent, state: roster.States()[at], text: report.Text})
		}
		return
	}
	if event.Kind == session.EventAttachment {
		var attached session.Attachment
		if json.Unmarshal(event.Body, &attached) == nil {
			l.loose = append(l.loose, attached)
		}
		return
	}
	if event.Agent != "" {
		if event.Kind == session.EventToolCall || event.Kind == session.EventToolResult {
			l.worked[event.Agent] = append(l.worked[event.Agent], event)
		}
		return
	}
	if event.Kind == session.EventMessage {
		var message session.MessageBody
		if json.Unmarshal(event.Body, &message) != nil {
			return
		}
		if message.Role == session.RoleUser {
			l.images[event.ID], l.loose = l.loose, nil
		}
		l.said = append(l.said, recordedMessage{logged: event, body: message})
		return
	}
	if event.Call == "" {
		return
	}
	recorded := l.calls[event.Call]
	if recorded == nil {
		recorded = &recordedCall{}
		l.calls[event.Call] = recorded
	}
	switch event.Kind {
	case session.EventToolCall:
		recorded.call = event
	case session.EventToolResult:
		recorded.result = event
	case session.EventSpawn:
		var body session.SpawnBody
		_ = json.Unmarshal(event.Body, &body)
		recorded.spawn = SubAgentRow{Started: event.At, Name: body.Agent, Agent: body.Definition, Model: body.Model, Owns: body.Owns, Ticket: body.Ticket, Doing: body.Mission, State: roster.Working}
	}
}

func callOf(event session.Event) llm.ToolCall {
	var call session.CallBody
	_ = json.Unmarshal(event.Body, &call)
	return llm.ToolCall{ID: event.Call, Name: call.Tool, Arguments: call.Args}
}

func resultOf(event session.Event) llm.Message {
	var result struct {
		session.ResultBody
		Hits *int `json:"hits"`
	}
	_ = json.Unmarshal(event.Body, &result)
	message := llm.Message{Role: llm.RoleTool, ToolCallID: event.Call, Content: turn.WithoutToolRef(result.Content), ToolResultBytes: result.ResultBytes, ToolExitCode: result.ExitCode, ToolHits: result.Hits}
	switch result.ToolOutcome {
	case "":
		message.ToolOutcome = llm.ToolOutcomeUnset
	case session.ToolOutcomeRan:
		message.ToolOutcome = llm.ToolOutcomeRan
	case session.ToolOutcomeAborted:
		message.ToolOutcome = llm.ToolOutcomeAborted
	default:
		message.ToolOutcome = llm.ToolOutcomeFailed
	}
	return message
}

func (l *resumedLead) spawned(row SubAgentRow) {
	l.show(row)
	for _, event := range l.worked[row.Name] {
		l.at = event.At
		if event.Kind == session.EventToolCall {
			l.watch.called(callOf(event), event.Agent)
		} else {
			l.watch.result(resultOf(event), event.Agent)
		}
	}
}

func recordedRuns(store *session.Store, id string) map[string]session.AgentRun {
	store = store.ReadOnce()
	lineage, _ := store.Ancestors(id)
	slices.Reverse(lineage)
	if header, err := store.Header(id); err == nil {
		lineage = append(lineage, header)
	}
	runs := map[string]session.AgentRun{}
	for _, from := range lineage {
		for _, run := range from.Agents {
			runs[run.Agent] = run
		}
	}
	return runs
}

func (l *resumedLead) reportsIn(task string) (string, bool) {
	drew := false
	for i := range l.reports {
		report := &l.reports[i]
		if report.drawn || !strings.Contains(task, report.text) || !l.linked(report.agent) {
			continue
		}
		at := slices.IndexFunc(l.rows, func(row SubAgentRow) bool { return row.Name == report.agent })
		l.rows[at].State, l.rows[at].Report, report.drawn = report.state, report.text, true
		l.rows[at].endedAs(l.runs[report.agent])
		l.show()
		task, drew = strings.Replace(task, report.text, "", 1), true
	}
	return strings.TrimSpace(task), drew
}

func (l *resumedLead) recorded(message llm.Message) *session.Event {
	if message.Role != llm.RoleUser && message.Role != llm.RoleAssistant {
		return nil
	}
	at := slices.IndexFunc(l.said, func(said recordedMessage) bool {
		return !said.taken && said.body.Role == message.Role.String() && said.body.Content == message.Content
	})
	if at < 0 {
		return nil
	}
	l.said[at].taken = true
	return &l.said[at].logged
}

func (l *resumedLead) originOf(message llm.Message) Origin {
	origin := message.Origin
	if at := slices.IndexFunc(l.reports, func(report recordedReport) bool { return strings.Contains(message.Content, report.text) }); origin.Kind() == OriginAgent && origin.Name == "" && at >= 0 {
		origin.Name = l.reports[at].agent
	}
	return wireOrigin(origin)
}

func (l *resumedLead) drawn(agent string) bool {
	return slices.ContainsFunc(l.rows, func(row SubAgentRow) bool { return row.Name == agent })
}

func (l *resumedLead) linked(agent string) bool {
	if l.drawn(agent) {
		return true
	}
	for _, recorded := range l.calls {
		if recorded.spawn.Name == agent && recorded.call.Call != "" {
			l.at = recorded.call.At
			l.watch.called(callOf(recorded.call), "")
			l.at = recorded.result.At
			l.watch.result(resultOf(recorded.result), "")
			l.spawned(recorded.spawn)
			return true
		}
	}
	return false
}

func (l *resumedLead) show(spawned ...SubAgentRow) {
	l.rows = append(l.rows, spawned...)
	l.watch.emit(Event{Kind: EventSubAgent, SubAgents: l.rows})
}

func (l *resumedLead) settleTheRunning(store *session.Store, id string) {
	if !slices.ContainsFunc(l.rows, func(row SubAgentRow) bool { return row.State == roster.Working }) {
		return
	}
	live := heldRoster(store, id)
	for _, held := range live {
		if held.State == roster.Working && !l.drawn(held.Name) {
			l.rows = append(l.rows, held)
		}
	}
	for i := range l.rows {
		row := &l.rows[i]
		if row.State != roster.Working {
			continue
		}
		if at := slices.IndexFunc(live, func(held SubAgentRow) bool { return held.Name == row.Name }); at >= 0 {
			row.State, row.Steps, row.Report = live[at].State, live[at].Steps, cmp.Or(live[at].Report, row.Report)
			continue
		}
		row.State = roster.Parked
		run := l.runs[row.Name]
		for _, ended := range []roster.State{roster.Errored, roster.Finished} {
			if ended.String() == run.Status {
				row.State = ended
				row.endedAs(run)
			}
		}
		for _, report := range l.reports {
			if report.agent == row.Name {
				row.Report = report.text
			}
		}
	}
	l.show()
}

func (carry Carry) taskIn(content string) string {
	if task, found := turn.TaskIn(content); found {
		return task
	}
	for _, task := range carry.Tasks {
		if task != "" && strings.HasSuffix(content, task) {
			return task
		}
	}
	_, after, _ := strings.Cut(content, envClose)
	return strings.TrimSpace(after)
}
