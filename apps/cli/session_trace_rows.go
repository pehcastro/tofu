package main

import (
	"cmp"
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/apps/cli/output"
	"tofu/apps/cli/wiring"
	"tofu/internal/agent/context/recall"
	"tofu/internal/agent/hooks"
	"tofu/internal/agent/loop"
	"tofu/internal/agent/subagents"
	"tofu/internal/cli/render"
	"tofu/internal/judge/ledger"
	"tofu/internal/models/llm/core"
	"tofu/internal/platform/fs"
	"tofu/internal/store/session"
	"tofu/internal/store/walls"
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
	forked := map[string]*loop.Fork{}
	walled, startup, stepSeparator := map[walledBy]int64{}, -1, ""
	estimates := map[string]session.Estimated{}
	if joined, err := session.Estimates(events); err == nil {
		for _, estimated := range joined {
			estimates[estimated.Event.ID] = estimated
		}
	}
	for _, event := range events {
		row := traceRow{At: event.At, Agent: event.Agent, Turn: event.Turn, ID: event.ID}
		by := walledBy{turn: event.Turn, agent: event.Agent}
		switch event.Kind {
		case session.EventWall:
			var wall walls.Wall
			if json.Unmarshal(event.Body, &wall) != nil {
				continue
			}
			switch wall.Op {
			case walls.OpStartup:
				row.Kind, row.Text = kindStartup, "startup took "+exactSpan(wall.MS)+" before the first request"
				if outsideMS := *cmp.Or(wall.OutsideMS, new(int64)); outsideMS > 0 {
					row.Text += ", " + exactSpan(outsideMS) + " of it outside tofu"
				}
				startup, stepSeparator = len(rows), ": "
			case walls.OpStartupStep:
				if startup >= 0 {
					rows[startup].Text += stepSeparator + wall.Name + " " + exactSpan(wall.MS)
					stepSeparator = " · "
				}
				continue
			case walls.OpTurn, walls.OpAdvisor:
				by.op = wall.Op
				walled[by] = wall.MS
				continue
			default:
				continue
			}
		case session.EventEstimate:
			row = estimateRow(row, estimates[event.ID])
		case session.EventPrompt:
			var prompt session.PromptBody
			_ = json.Unmarshal(event.Body, &prompt)
			for _, part := range prompt.Parts {
				what, name := wiring.LibraryItem(part)
				if what == "" {
					continue
				}
				libraryFiles[event.Agent+"\x00"+what+"\x00"+name] = part.File
				state := cmp.Or(string(part.State), "fired")
				switch {
				case part.Off:
					state = "switched off"
				case what == wiring.LibraryWhatAgent && part.Sent():
					state = "used"
				}
				row.Kind, row.State, row.Text = kindLibrary, state, traced(what+" "+name+" "+state, output.OneLine(part.Why), part.File)
				rows = append(rows, row)
			}
			continue
		case session.EventTurnStart:
			var start session.TurnStart
			_ = json.Unmarshal(event.Body, &start)
			row.Kind, row.State, row.Text, row.Origin = kindTurn, "began", traced("began: "+output.OneLine(start.Task), servedOn(start.Wire, start.Account)), start.From
		case session.EventTurnEnd:
			var ended struct {
				Outcome     session.Outcome `json:"outcome"`
				Error       string          `json:"error"`
				Wire        string          `json:"wire"`
				Account     int64           `json:"account"`
				WallClockMS int64           `json:"wall_clock_ms"`
				CostUSD     float64         `json:"total_cost_usd"`
			}
			_ = json.Unmarshal(event.Body, &ended)
			finished := ended.Outcome == session.OutcomeStopped || ended.Outcome == session.OutcomeUnset
			row.Kind, row.State, row.Text = kindTurn, ended.Outcome.String(), "ended"
			if !finished {
				row.Text += " on " + row.State
			}
			by.op = walls.OpTurn
			row.Text = traced(row.Text, servedOn(ended.Wire, ended.Account), took(cmp.Or(walled[by], ended.WallClockMS)), spent(ended.CostUSD), output.OneLine(ended.Error))
			delete(walled, by)
			row.Outcome = traceProblem(!finished && ended.Outcome != session.OutcomeError && ended.Outcome != session.OutcomeForked, ended.Error)
		case session.EventRequest:
			var step struct {
				session.StepBody
				Phase loop.Phase `json:"phase"`
			}
			_ = json.Unmarshal(event.Body, &step)
			exchange := sent[event.ID]
			delete(sent, event.ID)
			row.Kind, row.ID = kindRequest, cmp.Or(exchange.Request, event.ID)
			row.Text = traced(loop.ShortID(row.ID), cmp.Or(step.Model, exchange.Model), "in "+strconv.Itoa(step.PromptTokens)+" out "+strconv.Itoa(step.CompletionTokens)+" read "+strconv.Itoa(step.CacheReadTokens)+
				" wrote "+strconv.Itoa(step.CacheWriteTokens), took(exchange.DurationMS), spent(step.CostUSD), string(step.Phase), exchange.Why, output.OneLine(exchange.Error))
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
			row.ID, row.State, row.Text = event.Call, "unanswered", call.Tool+" "+output.OneLine(string(call.Args))
			calls[event.Call], asked[event.Call] = len(rows), call
			var message struct{ To, Do string }
			if call.Tool == "message" && json.Unmarshal(call.Args, &message) == nil && (message.Do == "kill" || message.Do == "stop") {
				causes = append(causes, stopCause{row: len(rows), took: []string{message.To}})
			}
			var ticketed struct{ ID, Ticket, Task, To, Reason string }
			switch {
			case call.Tool == loop.SpawnToolName && json.Unmarshal(call.Args, &ticketed) == nil:
				row.Ticket, row.work = ticketed.Ticket, workStep{what: workBrief, said: ticketed.Task}
			case call.Tool == loop.TicketMoveToolName && json.Unmarshal(call.Args, &ticketed) == nil:
				row.Ticket, row.work = ticketed.ID, workStep{what: workMove, said: traced("to "+ticketed.To, ticketed.Reason), to: ticketed.To}
			}
			if what, name := wiring.LibraryLoad(call); what != "" {
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
			var read struct{ Path string }
			var returned struct{ Command string }
			if call.Tool == "read" && json.Unmarshal(call.Args, &read) == nil && json.Unmarshal(event.Body, &returned) == nil {
				returned.Command = strings.TrimSpace(strings.TrimPrefix(returned.Command, read.Path))
			}
			answered.Text = traced(call.Tool+" → "+answered.State, returned.Command, ruleWords(result.Rules), sized(result.ResultBytes), took(result.DurationMS), output.OneLine(cmp.Or(reason, checkSummary(call.Tool, result.Content))), gate, output.OneLine(string(call.Args)))
			if result.ToolOutcome != session.ToolOutcomeSkipped && (result.Error != "" || result.Refused || result.ToolOutcome == session.ToolOutcomeFailed || result.ToolOutcome == session.ToolOutcomeAborted) {
				answered.Outcome = traceProblem(result.ToolOutcome == session.ToolOutcomeAborted, cmp.Or(reason, answered.State))
			}
			if what, name := wiring.LibraryLoad(call); what != "" {
				loaded, said, why := &rows[placed+1], "loaded by the model", ""
				loaded.State = string(session.PartLoaded)
				if answered.Outcome != "" {
					loaded.State, said, why = "refused", "refused", output.OneLine(cmp.Or(reason, answered.State))
				}
				loaded.Text = traced(what+" "+name+" "+said, why, libraryFiles[event.Agent+"\x00"+what+"\x00"+name])
			}
			if !result.Refused && (result.Verdict == "" || result.Verdict == string(ledger.VerdictAllow)) && (result.Reason == nil || result.Reason.RelaxedBy+result.Reason.AllowedBy == "") {
				continue
			}
			row.Kind, row.ID, row.State = kindGate, event.Call, cmp.Or(result.Verdict, "refused")
			row.Text = traced(call.Tool+": "+cmp.Or(gate, "refused"), output.OneLine(reason))
		case session.EventHook:
			var ran hooks.Run
			_ = json.Unmarshal(event.Body, &ran)
			row.Kind, row.ID = kindHook, event.Call
			row.Text = traced(string(ran.Event)+" "+ran.Command, "exit "+strconv.Itoa(ran.Exit)+" in "+strconv.FormatInt(ran.DurationMS, 10)+" ms",
				cmp.Or(output.OneLine(ran.Decision), "no decision"), output.OneLine(ran.Problem), output.OneLine(ran.Stderr))
			row.Outcome = traceProblem(false, ran.Problem)
		case session.EventMessage:
			var said session.MessageBody
			if json.Unmarshal(event.Body, &said) != nil || strings.TrimSpace(said.Content) == "" || said.Role != session.RoleUser && said.Role != session.RoleAssistant {
				continue
			}
			text, typed := loop.TaskIn(said.Content)
			if !typed {
				text = said.Content
			}
			row.Kind, row.State, row.Text = kindMessage, said.Role, said.Role+": "+output.OneLine(text)
			origin := loop.OriginOf(said)
			row.Origin = loop.RecordedOrigin(origin)
			if said.Origin != "" || said.From != nil {
				from := string(origin.Kind()) + "/" + string(origin.Source)
				row.Kind, row.State, row.Text = kindInsert, from, from+": "+traced(carriedFiles(origin.Source, said.Content), output.OneLine(text))
			}
			if origin.Source == core.SourceCronFire {
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
			row.Text = traced("spawned by "+cmp.Or(event.Agent, traceLead)+" as "+cmp.Or(spawned.Definition, "unnamed")+" on "+cmp.Or(spawned.Model, "the lead's model"), spawned.Ticket, output.OneLine(spawned.Mission), spawned.Picked, staggered)
		case session.EventAgentEnd:
			var ended session.AgentEndBody
			_ = json.Unmarshal(event.Body, &ended)
			row.Kind, row.State, row.Text, row.Ticket = kindSpawn, ended.Status, traced("ended "+ended.Status, spent(ended.CostUSD)), tickets[event.Agent]
			if ended.Status != subagents.Finished.String() {
				row.Kind, row.Outcome = kindStop, traceStopped
				unfinished = append(unfinished, len(rows))
			}
		case session.EventReport:
			var report session.ReportBody
			_ = json.Unmarshal(event.Body, &report)
			row.Kind, row.State, row.Text, row.Ticket = kindReport, report.State, report.State+": "+output.OneLine(report.Text), tickets[event.Agent]
			row.work = workStep{what: workReport, said: report.Text}
		case session.EventCompaction:
			var compacted struct {
				Compaction *loop.Compaction `json:"compaction"`
				Fork       *loop.Fork       `json:"fork"`
			}
			_ = json.Unmarshal(event.Body, &compacted)
			row.Kind, row.Text = kindCompaction, "compacted"
			if compacted.Compaction != nil {
				row.Text += " " + strconv.Itoa(compacted.Compaction.TokensBefore) + " to " + strconv.Itoa(compacted.Compaction.TokensAfter) + " tokens"
				for _, drop := range compacted.Compaction.Drops {
					held := " handle " + drop.Handle
					if drop.Path != "" {
						held = " picture " + drop.Path
					}
					row.Text = traced(row.Text, "cut "+drop.Tool+held+", "+strconv.Itoa(drop.Bytes)+" bytes, "+string(drop.Reason), drop.Decision)
				}
			}
			if compacted.Fork != nil {
				forked[event.Agent] = compacted.Fork
				row.Text = "forked at " + strconv.Itoa(compacted.Fork.TokensBefore) + " tokens, carried " + strconv.Itoa(compacted.Fork.TokensAfter)
			}
		case session.EventListChange:
			var change session.ListChangeBody
			_ = json.Unmarshal(event.Body, &change)
			counts := strconv.Itoa(len(change.Before)) + " to " + output.Plural(len(change.After), "message")
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
			var advised session.AdvisorNotice
			if json.Unmarshal(event.Body, &advised) == nil && advised.Advisor != nil {
				read := advised.Advisor
				var before struct {
					Advisor struct {
						DurationMS int64 `json:"duration_ms"`
					} `json:"advisor"`
				}
				_ = json.Unmarshal(event.Body, &before)
				by.op = walls.OpAdvisor
				row.Kind, row.Agent, row.State = kindAdvisor, read.Agent, "read"
				row.Text, row.Outcome = traced(output.OneLine(advised.Text), read.Model, took(cmp.Or(walled[by], before.Advisor.DurationMS)), spent(read.CostUSD)), traceProblem(false, read.Error)
				delete(walled, by)
				rows = append(rows, row)
				row.Outcome = ""
				for _, missing := range read.Missing {
					row.State, row.Text = "missing", "Missing: "+output.OneLine(missing)
					rows = append(rows, row)
				}
				for _, extra := range read.Extra {
					row.State, row.Text = "extra", "Extra: "+output.OneLine(extra)
					rows = append(rows, row)
				}
				continue
			}
			var notice session.StopBody
			_ = json.Unmarshal(event.Body, &notice)
			row.Kind, row.Text, row.Origin = kindNotice, output.OneLine(notice.Text), notice.From
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
			case event.Agent != "" && (notice.From == nil || core.Source(notice.From.Sub) == core.SourceGate):
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
				Outcome: traceProblem(false, exchange.Error), Text: traced(loop.ShortID(exchange.Request), exchange.Model, took(exchange.DurationMS), output.OneLine(exchange.Error))})
		}
	}
	for _, cut := range cacheTrace(events, exchanges).Breaks {
		rows = append(rows, traceRow{At: at[cut.Request], Kind: kindCache, Agent: cut.Agent, ID: cut.Request,
			Text: "read " + strconv.Itoa(cut.Read) + " of " + strconv.Itoa(cut.Cached) + " cached, " + cut.Gap + " after " + cut.After + ", first difference: " + cut.Differs})
	}
	return rows
}

var checkCounts = fs.LazyRegexp(`[^\s,;][^,;]*: \d+ passed, \d+ failed(, \d+ skipped)?|errors in [^,]*: \d+(, in other files: \d+)?`)

func checkSummary(tool, content string) string {
	if tool != "test" && tool != "typecheck" {
		return ""
	}
	return strings.Join(checkCounts().FindAllString(content, -1), "; ")
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

func carriedFiles(source core.Source, carry string) string {
	if source != core.SourceForkCarry {
		return ""
	}
	carried := recall.ReadCarried(carry)
	var said []string
	for _, file := range carried.Held {
		said = append(said, "carried "+file.Path+", "+strconv.Itoa(file.Bytes)+" bytes, call "+file.Call)
	}
	if carried.Unreadable {
		said = append(said, "files section unreadable after "+output.Plural(len(said), "file"))
	}
	if carried.Unseen != "" {
		said = append(said, "never seen "+carried.Unseen)
	}
	return strings.Join(said, " · ")
}

func forkWords(fork *loop.Fork) (step, carried string) {
	if fork == nil {
		return "step and files in the segment before, see --kind fork", ""
	}
	carried = "carried no file whole"
	if len(fork.Carry.Files) > 0 {
		carried = "carried " + strings.Join(fork.Carry.Files, ", ")
	}
	if len(fork.Carry.Facts) > 0 {
		facts := make([]string, len(fork.Carry.Facts))
		for i, fact := range fork.Carry.Facts {
			source, rest, _ := strings.Cut(strings.TrimPrefix(fact, "fact: "), " :: ")
			tool, _, _ := strings.Cut(rest, ",")
			facts[i] = source + " :: " + tool
		}
		carried = traced(carried, output.Plural(len(facts), "fact")+": "+strings.Join(facts, ", "))
	}
	return "after step " + strconv.Itoa(fork.Step), carried
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

func servedOn(wire string, account int64) string {
	if account == 0 {
		return ""
	}
	return cmp.Or(wireSources()[wire], wire) + " #" + strconv.FormatInt(account, 10)
}

func took(ms int64) string {
	switch {
	case ms == 0:
		return ""
	case time.Duration(ms)*time.Millisecond < time.Second:
		return strconv.FormatInt(ms, 10) + " ms"
	}
	return render.Until(time.Duration(ms) * time.Millisecond)
}

func sized(bytes int) string {
	if bytes == 0 {
		return ""
	}
	return render.Size(bytes)
}

func spent(usd float64) string {
	if usd == 0 {
		return ""
	}
	return dollars(usd)
}

func spanOf(ms int64) string { return render.Until(time.Duration(ms) * time.Millisecond) }

func exactSpan(ms int64) string { return (time.Duration(ms) * time.Millisecond).String() }

type walledBy struct {
	turn, agent string
	op          walls.Op
}

func estimateRow(row traceRow, estimated session.Estimated) traceRow {
	said := estimated.EstimateBody
	row.Kind, row.State = kindEstimate, "open"
	var where string
	switch said.Scope {
	case session.EstimateTurn:
		where = "lead turn"
	case session.EstimateAgent:
		row.Agent, where = said.Agent, "sub-agent "+said.Agent+" from call "+loop.ShortID(estimated.Event.Call)
	case session.EstimateCall:
		where = "call " + loop.ShortID(estimated.Event.Call)
	default:
		row.Text = traced("an estimate this build cannot read")
		return row
	}
	promised := []string{"said " + spanOf(said.SaidMS), "done by " + spanOf(said.DoneByMS)}
	var basis string
	switch said.Basis {
	case session.EstimateFromHistory:
		basis = "from " + output.Plural(said.Runs, "earlier run") + " here on " + said.Model
	case session.EstimateFirstRun:
		basis = "first run, from " + output.Plural(said.Runs, "run") + " in other projects on " + said.Model
	case session.EstimateNoHistory:
		basis, promised = "first run, no history on "+said.Model+" anywhere", []string{"no promise"}
	default:
		row.Text = traced(where, "an estimate this build cannot read")
		return row
	}
	parts := append([]string{where, basis}, promised...)
	settled := estimated.Settled
	if settled == nil {
		row.Text = traced(append(parts, "open: no settled row, so it was killed or the session closed mid-run")...)
		return row
	}
	row.State = string(settled.Outcome)
	promise, late := estimated.Promise()
	var verdict string
	switch promise {
	case session.PromiseKept:
		verdict = "in time"
	case session.PromiseLate:
		verdict = "late by " + spanOf(late)
	case session.PromiseNone, session.PromiseOpen:
	}
	row.Text = traced(append(parts, "took "+spanOf(settled.TookMS), string(settled.Outcome), verdict,
		"model "+spanOf(settled.ModelMS)+", tools "+spanOf(settled.ToolsMS)+", tests and builds "+spanOf(settled.TestMS)+" in "+output.Plural(settled.TestCalls, "call")+
			", permission "+spanOf(settled.PermissionMS)+" left out of the verdict, queue "+spanOf(settled.QueueMS))...)
	return row
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
