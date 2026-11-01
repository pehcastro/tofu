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
	carry   Carry
	store   *session.Store
	chat    []Event
	calls   map[string]*recordedCall
	reports []recordedReport
	rows    []SubAgentRow
	moves   map[string][]roster.Transition
	said    map[saidKey][]session.Event
	loose   []session.Attachment
	images  map[string][]session.Attachment
	runs    map[string]session.AgentRun
	worked  map[string][]session.Event
	turn    string
	at      time.Time
}

type saidKey struct {
	generation    int
	role, content string
}

type Opened struct {
	Chat    []Event
	Earlier *Earlier
}

type Earlier struct {
	carry   Carry
	mask    func(string) string
	store   *session.Store
	lineage []session.Generation
	runs    map[string]session.AgentRun
	bottom  int
	next    int
	child   []llm.Message
}

func resumedChat(carry Carry, dir string) Opened {
	earlier := &Earlier{carry: carry, runs: map[string]session.AgentRun{}, next: -1}
	if carry.Session == "" {
		return Opened{Earlier: earlier}
	}
	keys := carry.Keys
	if keys == nil {
		loaded := sys.LoadKeyRedactor()
		keys = &loaded
	}
	earlier.mask = keys.Redact
	labelled := Event{Kind: EventSession, Text: carry.Name, ID: carry.Session, Root: carry.Session}
	store, err := carry.reading(dir)
	if err != nil {
		lead := earlier.lead(nil)
		lead.watch.emit(labelled)
		lead.replay(carry.Messages, 0)
		return Opened{Chat: lead.chat, Earlier: earlier}
	}
	lead := earlier.lead(store)
	messages := earlier.open(store, lead)
	lead.watch.emit(labelledAs(store, labelled))
	store.ForgetRead()
	lead.replay(messages, len(earlier.lineage)-1)
	lead.settleTheRunning(func() []SubAgentRow { return heldRoster(store, carry.Session) })
	return Opened{Chat: lead.chat, Earlier: earlier}
}

func (e *Earlier) lead(store *session.Store) *resumedLead {
	lead := &resumedLead{carry: e.carry, store: store, calls: map[string]*recordedCall{}, moves: map[string][]roster.Transition{}, said: map[saidKey][]session.Event{}, worked: map[string][]session.Event{}, runs: e.runs, images: map[string][]session.Attachment{}}
	lead.watch = &watcher{emit: func(event Event) {
		event.Turn, event.At = lead.turn, lead.at
		lead.chat = append(lead.chat, redacted(event, e.mask))
	}, turnID: e.carry.Session, seen: &shownResults{ids: map[string]bool{}}, spawner: &turn.SpawnTool{}}
	return lead
}

func (e *Earlier) open(store *session.Store, lead *resumedLead) []llm.Message {
	e.lineage, _ = store.Lineage(e.carry.Session)
	head := len(e.lineage) - 1
	e.bottom = head
	for e.bottom > 0 && e.lineage[e.bottom-1].Header.ForkedInto == e.lineage[e.bottom].Header.ID {
		e.bottom--
	}
	for i, generation := range e.lineage {
		for _, event := range generation.Events {
			if i == head || event.Kind != session.EventMessage || len(lead.loose) > 0 {
				lead.note(event, i)
			}
		}
		for _, run := range generation.Header.Agents {
			e.runs[run.Agent] = run
		}
	}
	uncached := *store
	uncached.ForgetRead()
	e.store, e.next, e.child = &uncached, head-1, e.carry.Messages
	return e.unheldIn(head)
}

func (e *Earlier) Next() ([]Event, bool) {
	at := e.next
	if at < e.bottom {
		return nil, false
	}
	lead := e.lead(e.store)
	for _, event := range e.events(at) {
		lead.note(event, at)
	}
	lead.replay(e.unheldIn(at), at)
	lead.settleTheRunning(func() []SubAgentRow { return nil })
	e.lineage[at].Events, e.next = nil, at-1
	return lead.chat, e.next >= e.bottom
}

func (e *Earlier) unheldIn(at int) []llm.Message {
	if at <= e.bottom {
		return e.child
	}
	said, _, _ := e.store.ReadSaid(e.lineage[at-1].Header.ID, e.events(at-1))
	parent, _ := turn.ConversationOf(said)
	fresh := unheld(parent, e.child)
	e.child = parent
	return fresh
}

func (e *Earlier) events(at int) []session.Event {
	generation := &e.lineage[at]
	if generation.Unread {
		generation.Events, _ = e.store.Events(generation.Header.ID)
		generation.Unread = false
	}
	return generation.Events
}

func (l *resumedLead) replay(messages []llm.Message, generation int) {
	for _, message := range messages {
		if message.Origin.Source == llm.SourceSpawnLine {
			continue
		}
		said, persisted := l.recorded(message, generation), ""
		if said != nil {
			l.at, persisted = said.At, said.ID
		}
		if said != nil && message.Role == llm.RoleUser && (message.Origin.Source == llm.SourceTask || message.Origin.Source == llm.SourceCronFire) {
			l.turn, l.watch.turnID = said.Turn, cmp.Or(said.Turn, l.carry.Session)
		}
		switch message.Role {
		case llm.RoleUser:
			if endsTheRow(message.Origin.Source) {
				l.watch.endLeadRow()
			}
			task, origin := l.carry.taskIn(message.Content), l.originOf(message)
			left, drew := l.reportsIn(task)
			switch {
			case origin.Sub == llm.SourceEnvironment:
			case !drew || origin.Sub == llm.SourceReport || origin.Sub == llm.SourceReportFor:
				var images []MessageImage
				if said != nil && l.store != nil {
					images = shownAttached(task, l.images[said.ID], l.store)
				}
				l.watch.emit(Event{Kind: EventTask, ID: persisted, Text: task, Origin: origin, Images: images})
			case left != "" && origin.Kind == OriginPerson:
				l.watch.emit(Event{Kind: EventTask, ID: persisted, Text: left, Origin: origin})
			}
		case llm.RoleAssistant:
			if text := strings.TrimSpace(message.Content); text != "" {
				l.watch.endLeadRow()
				l.watch.emit(Event{Kind: EventText, ID: persisted, Text: text})
			}
			for _, call := range message.ToolCalls {
				recorded := l.calls[call.ID]
				if recorded != nil && recorded.call.Call != "" {
					l.replaying(recorded.call)
				}
				l.watch.called(call, "")
				if recorded != nil && recorded.spawn.Name != "" && !l.drawn(recorded.spawn.Name) {
					l.spawned(recorded.spawn)
				}
			}
		case llm.RoleTool:
			if recorded := l.calls[message.ToolCallID]; recorded != nil && recorded.result.Call != "" {
				ran := resultOf(recorded.result)
				message.ToolExitCode, message.ToolHits = ran.ToolExitCode, ran.ToolHits
				l.replaying(recorded.result)
			}
			l.watch.result(message, "")
		case llm.RoleSystem, llm.RoleUnknown:
		}
		if said != nil {
			l.watch.emit(Event{Kind: EventPersisted, ID: said.ID, Logged: said})
		}
	}
}

func unheld(parent, child []llm.Message) []llm.Message {
	held := map[string]bool{}
	for _, message := range parent {
		held[heldKey(message)] = true
	}
	carry := slices.IndexFunc(child, func(message llm.Message) bool { return message.Origin.Source == llm.SourceForkCarry })
	fresh := carry + 1
	for fresh < len(child) && held[heldKey(child[fresh])] {
		fresh++
	}
	if carry < 0 {
		return child[fresh:]
	}
	return append([]llm.Message{child[carry]}, child[fresh:]...)
}

func heldKey(message llm.Message) string {
	switch {
	case message.Role == llm.RoleTool:
		return "tool " + message.ToolCallID
	case len(message.ToolCalls) > 0:
		return "call " + message.ToolCalls[0].ID
	}
	return message.Role.String() + " " + message.Content
}

func (l *resumedLead) note(event session.Event, generation int) {
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
	if event.Kind == session.EventNotice {
		var notice struct {
			Transition roster.Transition `json:"transition"`
		}
		if json.Unmarshal(event.Body, &notice) == nil && notice.Transition.Agent != "" {
			moved := notice.Transition.Agent
			l.moves[moved] = append(l.moves[moved], notice.Transition)
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
		key := saidKey{generation: generation, role: message.Role, content: message.Content}
		l.said[key] = append(l.said[key], event)
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

func (l *resumedLead) recorded(message llm.Message, generation int) *session.Event {
	if message.Role != llm.RoleUser && message.Role != llm.RoleAssistant {
		return nil
	}
	key := saidKey{generation: generation, role: message.Role.String(), content: message.Content}
	queued := l.said[key]
	if len(queued) == 0 {
		return nil
	}
	l.said[key] = queued[1:]
	return &queued[0]
}

func (l *resumedLead) originOf(message llm.Message) Origin {
	origin := message.Origin
	if origin.Kind() != OriginAgent || origin.Name != "" {
		return wireOrigin(origin)
	}
	if at := slices.IndexFunc(l.reports, func(report recordedReport) bool { return strings.Contains(message.Content, report.text) }); at >= 0 {
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
			l.replaying(recorded.call)
			l.watch.called(callOf(recorded.call), "")
			l.replaying(recorded.result)
			l.watch.result(resultOf(recorded.result), "")
			l.spawned(recorded.spawn)
			return true
		}
	}
	return false
}

func (l *resumedLead) replaying(logged session.Event) {
	l.at, l.watch.turnID = logged.At, cmp.Or(logged.Turn, l.watch.turnID)
}

func (l *resumedLead) show(spawned ...SubAgentRow) {
	l.rows = append(l.rows, spawned...)
	for i := range l.rows {
		if len(l.rows[i].Moves) == 0 {
			l.rows[i].Moves = l.moves[l.rows[i].Name]
		}
	}
	l.watch.emit(Event{Kind: EventSubAgent, SubAgents: l.rows})
}

func (l *resumedLead) settleTheRunning(held func() []SubAgentRow) {
	if !slices.ContainsFunc(l.rows, func(row SubAgentRow) bool { return row.State == roster.Working }) {
		return
	}
	live := held()
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
