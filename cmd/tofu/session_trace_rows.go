package main

import (
	"cmp"
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/internal/hook"
	"tofu/internal/judge/ledger"
	"tofu/internal/llm"
	"tofu/internal/recall"
	"tofu/internal/rule"
	"tofu/internal/session"
	"tofu/internal/subagent"
	"tofu/internal/sys"
	"tofu/internal/turn"
	"tofu/internal/widget"
)

func traceRows(events []session.Event, exchanges []session.Exchange) []traceRow {
	sent, at := map[string]session.Exchange{}, map[string]time.Time{}
	for _, exchange := range exchanges {
		sent[exchange.Request], at[exchange.Request] = exchange, exchange.At
	}
	var rows []traceRow
	var causes []stopCause
	var unfinished []int
	calls, asked := map[string]int{}, map[string]session.CallBody{}
	libraryFiles, tickets := map[string]string{}, map[string]string{}
	forked := map[string]*turn.Fork{}
	for _, event := range events {
		row := traceRow{At: event.At, Agent: event.Agent, Turn: event.Turn, ID: event.ID}
		switch event.Kind {
		case session.EventPrompt:
			var prompt session.PromptBody
			_ = json.Unmarshal(event.Body, &prompt)
			for _, part := range prompt.Parts {
				what, name := libraryItem(part)
				if what == "" {
					continue
				}
				libraryFiles[event.Agent+"\x00"+what+"\x00"+name] = part.File
				state := cmp.Or(string(part.State), "fired")
				switch {
				case part.Off:
					state = "switched off"
				case what == libraryWhatAgent && part.Sent():
					state = "used"
				}
				row.Kind, row.State, row.Text = kindLibrary, state, traced(what+" "+name+" "+state, oneLine(part.Why), part.File)
				rows = append(rows, row)
			}
			continue
		case session.EventTurnStart:
			var start session.TurnStart
			_ = json.Unmarshal(event.Body, &start)
			row.Kind, row.State, row.Text, row.Origin = kindTurn, "began", "began: "+oneLine(start.Task), start.From
		case session.EventTurnEnd:
			var ended struct {
				Outcome     session.Outcome `json:"outcome"`
				Error       string          `json:"error"`
				WallClockMS int64           `json:"wall_clock_ms"`
				CostUSD     float64         `json:"total_cost_usd"`
			}
			_ = json.Unmarshal(event.Body, &ended)
			finished := ended.Outcome == session.OutcomeStopped || ended.Outcome == session.OutcomeUnset
			row.Kind, row.State, row.Text = kindTurn, ended.Outcome.String(), "ended"
			if !finished {
				row.Text += " on " + row.State
			}
			row.Text = traced(row.Text, took(ended.WallClockMS), spent(ended.CostUSD), oneLine(ended.Error))
			row.Outcome = traceProblem(!finished && ended.Outcome != session.OutcomeError && ended.Outcome != session.OutcomeForked, ended.Error)
		case session.EventRequest:
			var step struct {
				session.StepBody
				Phase turn.Phase `json:"phase"`
			}
			_ = json.Unmarshal(event.Body, &step)
			exchange := sent[event.ID]
			delete(sent, event.ID)
			row.Kind, row.ID = kindRequest, cmp.Or(exchange.Request, event.ID)
			row.Text = traced(turn.ShortID(row.ID), cmp.Or(step.Model, exchange.Model), "in "+strconv.Itoa(step.PromptTokens)+" out "+strconv.Itoa(step.CompletionTokens)+" read "+strconv.Itoa(step.CacheReadTokens)+
				" wrote "+strconv.Itoa(step.CacheWriteTokens), took(exchange.DurationMS), spent(step.CostUSD), string(step.Phase), exchange.Why, oneLine(exchange.Error))
			row.Outcome = traceProblem(false, exchange.Error)
		case session.EventMemoryRequest:
			var step session.StepBody
			_ = json.Unmarshal(event.Body, &step)
			row.ID = cmp.Or(event.Request, event.ID)
			row.Kind, row.Text = kindMemory, traced(step.Model, "in "+strconv.Itoa(step.PromptTokens)+" out "+strconv.Itoa(step.CompletionTokens), spent(step.CostUSD), sent[row.ID].Why)
			delete(sent, row.ID)
		case session.EventToolCall:
			var call session.CallBody
			_ = json.Unmarshal(event.Body, &call)
			row.Kind = cmp.Or(map[string]traceKind{"bash": kindShell, "shell": kindShell, "cron": kindCron}[call.Tool], kindCall)
			row.ID, row.State, row.Text = event.Call, "unanswered", call.Tool+" "+oneLine(string(call.Args))
			calls[event.Call], asked[event.Call] = len(rows), call
			var message struct{ To, Do string }
			if call.Tool == "message" && json.Unmarshal(call.Args, &message) == nil && (message.Do == "kill" || message.Do == "stop") {
				causes = append(causes, stopCause{row: len(rows), took: []string{message.To}})
			}
			var ticketed struct{ ID, Ticket, Task, To, Reason string }
			switch {
			case call.Tool == turn.SpawnToolName && json.Unmarshal(call.Args, &ticketed) == nil:
				row.Ticket, row.work = ticketed.Ticket, workStep{what: workBrief, said: ticketed.Task}
			case call.Tool == turn.TicketMoveToolName && json.Unmarshal(call.Args, &ticketed) == nil:
				row.Ticket, row.work = ticketed.ID, workStep{what: workMove, said: traced("to "+ticketed.To, ticketed.Reason), to: ticketed.To}
			}
			if what, name := libraryLoad(call); what != "" {
				rows = append(rows, row)
				row.Kind, row.State = kindLibrary, "asked"
				row.Text = traced(what+" "+name+" asked by the model", libraryFiles[event.Agent+"\x00"+what+"\x00"+name])
			}
		case session.EventToolResult:
			var result traceResult
			_ = json.Unmarshal(event.Body, &result)
			placed, known := calls[event.Call]
			if !known {
				continue
			}
			answered, call := &rows[placed], asked[event.Call]
			answered.State = cmp.Or(result.ToolOutcome, session.ToolOutcomeRan)
			if result.Refused {
				answered.State = "refused"
			}
			reason, gate := callReason(result.ResultBody), gateWords(result, nil, event.Call)
			answered.Text = traced(call.Tool+" → "+answered.State, ruleWords(result.Rules), sized(result.ResultBytes), took(result.DurationMS), oneLine(cmp.Or(reason, checkSummary(call.Tool, result.Content))), gate, oneLine(string(call.Args)))
			if result.ToolOutcome != session.ToolOutcomeSkipped && (result.Error != "" || result.Refused || result.ToolOutcome == session.ToolOutcomeFailed || result.ToolOutcome == session.ToolOutcomeAborted) {
				answered.Outcome = traceProblem(result.ToolOutcome == session.ToolOutcomeAborted, cmp.Or(reason, answered.State))
			}
			if what, name := libraryLoad(call); what != "" {
				loaded, said, why := &rows[placed+1], "loaded by the model", ""
				loaded.State = string(session.PartLoaded)
				if answered.Outcome != "" {
					loaded.State, said, why = "refused", "refused", oneLine(cmp.Or(reason, answered.State))
				}
				loaded.Text = traced(what+" "+name+" "+said, why, libraryFiles[event.Agent+"\x00"+what+"\x00"+name])
			}
			if !result.Refused && (result.Verdict == "" || result.Verdict == string(ledger.VerdictAllow)) && (result.Reason == nil || result.Reason.RelaxedBy+result.Reason.AllowedBy == "") {
				continue
			}
			row.Kind, row.ID, row.State = kindGate, event.Call, cmp.Or(result.Verdict, "refused")
			row.Text = traced(call.Tool+": "+cmp.Or(gate, "refused"), oneLine(reason))
		case session.EventHook:
			var ran hook.Run
			_ = json.Unmarshal(event.Body, &ran)
			row.Kind, row.ID = kindHook, event.Call
			row.Text = traced(string(ran.Event)+" "+ran.Command, "exit "+strconv.Itoa(ran.Exit)+" in "+strconv.FormatInt(ran.DurationMS, 10)+" ms",
				cmp.Or(oneLine(ran.Decision), "no decision"), oneLine(ran.Problem), oneLine(ran.Stderr))
			row.Outcome = traceProblem(false, ran.Problem)
		case session.EventMessage:
			var said session.MessageBody
			if json.Unmarshal(event.Body, &said) != nil || strings.TrimSpace(said.Content) == "" || said.Role != session.RoleUser && said.Role != session.RoleAssistant {
				continue
			}
			text, typed := turn.TaskIn(said.Content)
			if !typed {
				text = said.Content
			}
			row.Kind, row.State, row.Text = kindMessage, said.Role, said.Role+": "+oneLine(text)
			origin := turn.OriginOf(said)
			row.Origin = turn.RecordedOrigin(origin)
			if said.Origin != "" || said.From != nil {
				from := string(origin.Kind()) + "/" + string(origin.Source)
				row.Kind, row.State, row.Text = kindInsert, from, from+": "+traced(carriedFiles(origin.Source, said.Content), oneLine(text))
			}
			if origin.Source == llm.SourceCronFire {
				row.Kind = kindCron
			}
		case session.EventSpawn:
			var spawned session.SpawnBody
			_ = json.Unmarshal(event.Body, &spawned)
			row.Kind, row.Agent, row.State, row.Ticket = kindSpawn, spawned.Agent, "spawned", spawned.Ticket
			tickets[spawned.Agent] = spawned.Ticket
			staggered := ""
			if spawned.Stagger != "" {
				staggered = "stagger " + spawned.Stagger + ": " + spawned.StaggerWhy
			}
			row.Text = traced("spawned by "+cmp.Or(event.Agent, traceLead)+" as "+cmp.Or(spawned.Definition, "unnamed")+" on "+cmp.Or(spawned.Model, "the lead's model"), spawned.Ticket, oneLine(spawned.Mission), staggered)
		case session.EventAgentEnd:
			var ended session.AgentEndBody
			_ = json.Unmarshal(event.Body, &ended)
			row.Kind, row.State, row.Text, row.Ticket = kindSpawn, ended.Status, traced("ended "+ended.Status, spent(ended.CostUSD)), tickets[event.Agent]
			if ended.Status != subagent.Finished.String() {
				row.Kind, row.Outcome = kindStop, traceStopped
				unfinished = append(unfinished, len(rows))
			}
		case session.EventReport:
			var report session.ReportBody
			_ = json.Unmarshal(event.Body, &report)
			row.Kind, row.State, row.Text, row.Ticket = kindReport, report.State, report.State+": "+oneLine(report.Text), tickets[event.Agent]
			row.work = workStep{what: workReport, said: report.Text}
		case session.EventCompaction:
			var compacted struct {
				Compaction *turn.Compaction `json:"compaction"`
				Fork       *turn.Fork       `json:"fork"`
			}
			_ = json.Unmarshal(event.Body, &compacted)
			row.Kind, row.Text = kindCompaction, "compacted"
			if compacted.Compaction != nil {
				row.Text += " " + strconv.Itoa(compacted.Compaction.TokensBefore) + " to " + strconv.Itoa(compacted.Compaction.TokensAfter) + " tokens"
				for _, drop := range compacted.Compaction.Drops {
					row.Text = traced(row.Text, "cut "+drop.Tool+" handle "+drop.Handle+", "+strconv.Itoa(drop.Bytes)+" bytes, "+string(drop.Reason), drop.Decision)
				}
			}
			if compacted.Fork != nil {
				forked[event.Agent] = compacted.Fork
				row.Text = "forked at " + strconv.Itoa(compacted.Fork.TokensBefore) + " tokens, carried " + strconv.Itoa(compacted.Fork.TokensAfter)
			}
		case session.EventListChange:
			var change session.ListChangeBody
			_ = json.Unmarshal(event.Body, &change)
			counts := strconv.Itoa(len(change.Before)) + " to " + plural(len(change.After), "message")
			row.Kind, row.Text = kindList, traced(change.Kind+" "+counts, change.Why)
			if change.Kind == session.ListChangeFork {
				step, carried := forkWords(forked[event.Agent])
				row.Kind, row.Text = kindFork, traced(step, counts, carried, change.Why)
				delete(forked, event.Agent)
			}
		case session.EventRead:
			var read session.Read
			_ = json.Unmarshal(event.Body, &read)
			row.Kind, row.Text, row.Outcome = kindRead, traced(read.Tool+" "+read.Source, read.Span, sized(read.Bytes), read.Error), traceProblem(false, read.Error)
		case session.EventNotice:
			var notice session.StopBody
			_ = json.Unmarshal(event.Body, &notice)
			row.Kind, row.Text, row.Origin = kindNotice, oneLine(notice.Text), notice.From
			spawnOf, timed := strings.CutPrefix(notice.Text, "spawn of ")
			switch {
			case notice.Stop != nil:
				row.Kind, row.State, row.Asked = kindStop, string(notice.Stop.Kind), notice.Stop.By
				if notice.Stop.Kind != session.TurnEnd {
					row.Outcome = traceStopped
				}
				causes = append(causes, stopCause{row: len(rows), took: notice.Stop.Took})
			case strings.HasPrefix(notice.Text, "startup took"):
				row.Kind = kindStartup
			case strings.HasPrefix(notice.Text, "[memory#"):
				row.Kind = kindMemory
			case timed:
				row.Kind = kindSpawn
				row.Agent, _, _ = strings.Cut(spawnOf, " ")
			case event.Agent != "" && (notice.From == nil || llm.Source(notice.From.Sub) == llm.SourceGate):
				row.Kind = kindGate
			}
		case session.EventAttachment:
			var attached session.Attachment
			_ = json.Unmarshal(event.Body, &attached)
			row.Kind, row.Text = kindAttachment, traced(attached.File, sized(attached.Bytes))
		default:
			continue
		}
		rows = append(rows, row)
	}
	for _, at := range unfinished {
		rows[at].Text = traced(rows[at].Text, becauseOf(rows, causes, at))
	}
	for _, exchange := range exchanges {
		if _, unrecorded := sent[exchange.Request]; unrecorded && exchange.Error != "" {
			rows = append(rows, traceRow{At: exchange.At, Kind: kindRequest, Agent: exchange.Agent, Turn: exchange.Turn, ID: exchange.Request,
				Outcome: traceProblem(false, exchange.Error), Text: traced(turn.ShortID(exchange.Request), exchange.Model, took(exchange.DurationMS), oneLine(exchange.Error))})
		}
	}
	for _, cut := range cacheTrace(events, exchanges).Breaks {
		rows = append(rows, traceRow{At: at[cut.Request], Kind: kindCache, Agent: cut.Agent, ID: cut.Request,
			Text: "read " + strconv.Itoa(cut.Read) + " of " + strconv.Itoa(cut.Cached) + " cached, " + cut.Gap + " after " + cut.After + ", first difference: " + cut.Differs})
	}
	return rows
}

var checkCounts = sys.LazyRegexp(`[^\s,;][^,;]*: \d+ passed, \d+ failed(, \d+ skipped)?|errors in [^,]*: \d+(, in other files: \d+)?`)

func checkSummary(tool, content string) string {
	if tool != "test" && tool != "typecheck" {
		return ""
	}
	return strings.Join(checkCounts().FindAllString(content, -1), "; ")
}

const (
	libraryWhatRule      = "rule"
	libraryWhatAgent     = "agent"
	libraryWhatSkill     = "skill"
	libraryWhatReference = "reference"
)

func libraryItem(part session.PromptPart) (string, string) {
	switch {
	case part.Rule != "":
		return libraryWhatRule, part.Rule
	case part.Kind == string(rule.ConcernIdentity) && (part.Agent != "" || part.File != ""):
		return libraryWhatAgent, cmp.Or(part.Agent, "unnamed")
	case part.Kind == string(turn.ConcernSkills) && part.Name != "":
		return libraryWhatSkill, part.Name
	case part.Kind == string(turn.ConcernReferences):
		return libraryWhatReference, part.Name
	}
	return "", ""
}

func libraryLoad(call session.CallBody) (string, string) {
	var named struct{ Name string }
	what := map[string]string{turn.SkillToolName: libraryWhatSkill, turn.ReferenceToolName: libraryWhatReference}[call.Tool]
	if what == "" || json.Unmarshal(call.Args, &named) != nil {
		return "", ""
	}
	return what, named.Name
}

type stopCause struct {
	row  int
	took []string
}

func becauseOf(rows []traceRow, causes []stopCause, at int) string {
	agent := rows[at].Agent
	for i := len(causes) - 1; i >= 0; i-- {
		cause := rows[causes[i].row]
		if causes[i].row > at || !slices.Contains(causes[i].took, agent) {
			continue
		}
		if cause.Kind == kindCall {
			cause.Text = cause.who() + " called " + cause.Text
		}
		return "because of " + cause.Text + " (row " + cause.ID + ")"
	}
	for i := at - 1; i >= 0; i-- {
		if rows[i].Agent != agent || rows[i].Outcome == "" {
			continue
		}
		said := "after " + rows[i].Text + " (row " + rows[i].ID + ")"
		if rows[i].Outcome == traceStopped {
			said += ", and no stop row names who asked"
		}
		return said
	}
	return "no row before it says why"
}

func carriedFiles(source llm.Source, carry string) string {
	if source != llm.SourceForkCarry {
		return ""
	}
	carried := recall.ReadCarried(carry)
	var said []string
	for _, file := range carried.Held {
		said = append(said, "carried "+file.Path+", "+strconv.Itoa(file.Bytes)+" bytes, call "+file.Call)
	}
	if carried.Unreadable {
		said = append(said, "files section unreadable after "+plural(len(said), "file"))
	}
	if carried.Unseen != "" {
		said = append(said, "never seen "+carried.Unseen)
	}
	return strings.Join(said, " · ")
}

func forkWords(fork *turn.Fork) (step, carried string) {
	switch {
	case fork == nil:
		return "step and files in the segment before, see --kind fork", ""
	case len(fork.Carry.Files) == 0:
		return "after step " + strconv.Itoa(fork.Step), "carried no file whole"
	}
	return "after step " + strconv.Itoa(fork.Step), "carried " + strings.Join(fork.Carry.Files, ", ")
}

func ruleWords(found []session.RuleFinding) string {
	words := make([]string, len(found))
	for i, one := range found {
		words[i] = "rule " + one.Rule + " " + one.Mode + " at " + one.At
	}
	return strings.Join(words, ", ")
}

func traced(parts ...string) string {
	return strings.Join(slices.DeleteFunc(parts, func(part string) bool { return part == "" }), " · ")
}

func took(ms int64) string {
	switch {
	case ms == 0:
		return ""
	case time.Duration(ms)*time.Millisecond < time.Second:
		return strconv.FormatInt(ms, 10) + " ms"
	}
	return widget.Until(time.Duration(ms) * time.Millisecond)
}

func sized(bytes int) string {
	if bytes == 0 {
		return ""
	}
	return widget.Size(bytes)
}

func spent(usd float64) string {
	if usd == 0 {
		return ""
	}
	return dollars(usd)
}

func traceProblem(stopped bool, failure string) traceOutcome {
	switch {
	case stopped || strings.Contains(failure, context.Canceled.Error()):
		return traceStopped
	case failure != "":
		return traceFailed
	}
	return ""
}
