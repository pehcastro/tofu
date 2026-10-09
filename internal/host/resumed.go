package host

import (
	"cmp"
	"encoding/json"
	"slices"
	"strings"

	"tofu/internal/llm"
	"tofu/internal/session"
	roster "tofu/internal/subagent"
	"tofu/internal/sys"
	"tofu/internal/turn"
)

type recordedCall struct {
	call   llm.ToolCall
	result llm.Message
	spawn  SubAgentRow
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
	origins map[string]string
	rows    []SubAgentRow
	said    []recordedMessage
	runs    map[string]session.AgentRun
	worked  map[string][]session.Event
	turn    string
}

type recordedMessage struct {
	logged session.Event
	body   session.MessageBody
	taken  bool
}

const (
	typedByThePerson   = "typed by the person"
	steeredByThePerson = "steer"
	cronSource         = "cron "
	reportSource       = "sub-agent report"
	gateAskSource      = "sub-agent gate ask"
	taskSource         = "task"
	forkTaskSource     = "fork task"
	forkCarrySource    = "fork carry"
)

func resumedChat(carry Carry, dir string) []Event {
	if carry.Session == "" {
		return nil
	}
	var chat []Event
	mask := sys.LoadKeyRedactor().Redact
	lead := &resumedLead{calls: map[string]*recordedCall{}, origins: map[string]string{}, worked: map[string][]session.Event{}}
	lead.watch = &watcher{emit: func(event Event) {
		event.Turn = lead.turn
		chat = append(chat, redacted(event, mask))
	}, turnID: carry.Session, seen: map[string]bool{}, spawner: &turn.SpawnTool{}}
	labelled := Event{Kind: EventSession, Text: carry.Name, ID: carry.Session, Root: carry.Session}
	if store, err := carry.reading(dir); err == nil {
		lead.read(store, carry.Session)
		lead.runs = recordedRuns(store, carry.Session)
		store.ForgetRead()
		labelled = labelledAs(store, labelled)
	}
	lead.watch.emit(labelled)
	for _, message := range carry.Messages {
		said := lead.recorded(message)
		if said != nil && message.Role == llm.RoleUser && startsATurn(lead.origins[message.Content]) {
			lead.turn, lead.watch.turnID = said.Turn, cmp.Or(said.Turn, carry.Session)
		}
		switch message.Role {
		case llm.RoleUser:
			task := carry.taskIn(message.Content)
			if left, drew := lead.reportsIn(task); !drew {
				lead.watch.emit(Event{Kind: EventTask, Text: task, Origin: lead.originOf(message.Content)})
			} else if left != "" && lead.typed(message.Content) {
				lead.watch.emit(Event{Kind: EventTask, Text: left, Origin: Origin{Kind: OriginPerson}})
			}
		case llm.RoleAssistant:
			if text := strings.TrimSpace(message.Content); text != "" {
				lead.watch.emit(Event{Kind: EventText, Text: text})
			}
			for _, call := range message.ToolCalls {
				lead.watch.called(call, "")
				if recorded := lead.calls[call.ID]; recorded != nil && recorded.spawn.Name != "" && !lead.drawn(recorded.spawn.Name) {
					lead.spawned(recorded.spawn)
				}
			}
		case llm.RoleTool:
			lead.watch.result(message, "")
		case llm.RoleSystem, llm.RoleUnknown:
		}
		if said != nil {
			lead.watch.emit(Event{Kind: EventPersisted, ID: said.ID, Logged: said})
		}
	}
	lead.parkTheRunning()
	return chat
}

func (l *resumedLead) read(store *session.Store, id string) {
	header, err := store.Header(id)
	if err != nil {
		return
	}
	lineage, _ := store.Ancestors(id)
	slices.Reverse(lineage)
	for _, from := range append(lineage, header) {
		events, _ := store.Events(from.ID)
		for _, event := range events {
			l.note(event)
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
			l.origins[message.Content] = message.Origin
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
		recorded.call = callOf(event)
	case session.EventToolResult:
		recorded.result = resultOf(event)
	case session.EventSpawn:
		var body session.SpawnBody
		_ = json.Unmarshal(event.Body, &body)
		recorded.spawn = SubAgentRow{Started: event.At, Name: body.Agent, Agent: body.Definition, Model: body.Model, Owns: body.Owns, Doing: body.Mission, State: roster.Working}
	}
}

func callOf(event session.Event) llm.ToolCall {
	var call session.CallBody
	_ = json.Unmarshal(event.Body, &call)
	return llm.ToolCall{ID: event.Call, Name: call.Tool, Arguments: call.Args}
}

func resultOf(event session.Event) llm.Message {
	var result session.ResultBody
	_ = json.Unmarshal(event.Body, &result)
	return llm.Message{Role: llm.RoleTool, ToolCallID: event.Call, Content: result.Content, ToolResultBytes: result.ResultBytes}
}

func startsATurn(source string) bool {
	return source == "" || source == taskSource || strings.HasPrefix(source, cronSource)
}

func (l *resumedLead) spawned(row SubAgentRow) {
	l.show(row)
	for _, event := range l.worked[row.Name] {
		if event.Kind == session.EventToolCall {
			l.watch.called(callOf(event), event.Agent)
		} else {
			l.watch.result(resultOf(event), event.Agent)
		}
	}
}

func recordedRuns(store *session.Store, id string) map[string]session.AgentRun {
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

func (l *resumedLead) typed(content string) bool { return typedSource(l.origins[content]) }

func typedSource(origin string) bool {
	return origin == "" || strings.Contains(origin, typedByThePerson) || strings.Contains(origin, steeredByThePerson)
}

func (o Origin) recorded() string {
	if o.Kind != OriginCron {
		return ""
	}
	return cronSource + o.Job + " " + o.Schedule
}

func (l *resumedLead) originOf(content string) Origin {
	origin := originFrom(l.origins[content])
	if at := slices.IndexFunc(l.reports, func(report recordedReport) bool { return strings.Contains(content, report.text) }); origin.Kind == OriginAgent && at >= 0 {
		origin.Name = l.reports[at].agent
	}
	return origin
}

func originFrom(source string) Origin {
	if fired, isCron := strings.CutPrefix(source, cronSource); isCron {
		job, schedule, _ := strings.Cut(fired, " ")
		return Origin{Kind: OriginCron, Job: job, Schedule: schedule}
	}
	switch {
	case typedSource(source) || slices.Contains([]string{taskSource, forkTaskSource, forkCarrySource}, source):
		return Origin{Kind: OriginPerson}
	case strings.Contains(source, reportSource) || strings.Contains(source, gateAskSource):
		return Origin{Kind: OriginAgent}
	}
	return Origin{Kind: OriginTofu, Source: source}
}

func (l *resumedLead) drawn(agent string) bool {
	return slices.ContainsFunc(l.rows, func(row SubAgentRow) bool { return row.Name == agent })
}

func (l *resumedLead) linked(agent string) bool {
	if l.drawn(agent) {
		return true
	}
	for _, recorded := range l.calls {
		if recorded.spawn.Name == agent && recorded.call.ID != "" {
			l.watch.called(recorded.call, "")
			l.watch.result(recorded.result, "")
			l.spawned(recorded.spawn)
			return true
		}
	}
	return false
}

func (l *resumedLead) show(spawned ...SubAgentRow) {
	l.rows = append(l.rows, spawned...)
	l.watch.emit(Event{Kind: EventSubAgent, SubAgents: slices.Clone(l.rows)})
}

func (l *resumedLead) parkTheRunning() {
	parked := false
	for i := range l.rows {
		if l.rows[i].State == roster.Working {
			l.rows[i].State, parked = roster.Parked, true
		}
	}
	if parked {
		l.show()
	}
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
