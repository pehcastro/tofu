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
	"tofu/internal/session"
	"tofu/internal/subagent"
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
	for _, event := range events {
		row := traceRow{At: event.At, Agent: event.Agent, Turn: event.Turn, ID: event.ID}
		switch event.Kind {
		case session.EventTurnStart:
			var start session.TurnStart
			_ = json.Unmarshal(event.Body, &start)
			row.Kind, row.State, row.Text = kindTurn, "began", "began: "+oneLine(start.Task)
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
			row.Text = traced(cmp.Or(step.Model, exchange.Model), "in "+strconv.Itoa(step.PromptTokens)+" out "+strconv.Itoa(step.CompletionTokens)+" read "+strconv.Itoa(step.CacheReadTokens)+
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
			answered.Text = traced(call.Tool+" → "+answered.State, sized(result.ResultBytes), took(result.DurationMS), oneLine(reason), gate, oneLine(string(call.Args)))
			if result.Error != "" || result.Refused || result.ToolOutcome == session.ToolOutcomeFailed || result.ToolOutcome == session.ToolOutcomeAborted {
				answered.Outcome = traceProblem(result.ToolOutcome == session.ToolOutcomeAborted, cmp.Or(reason, answered.State))
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
			if said.Origin != "" || said.From != nil {
				from := string(origin.Kind()) + "/" + string(origin.Source)
				row.Kind, row.State, row.Text = kindInsert, from, from+": "+oneLine(text)
			}
			if origin.Source == llm.SourceCronFire {
				row.Kind = kindCron
			}
		case session.EventSpawn:
			var spawned session.SpawnBody
			_ = json.Unmarshal(event.Body, &spawned)
			row.Kind, row.Agent, row.State = kindSpawn, spawned.Agent, "spawned"
			row.Text = traced("spawned by "+cmp.Or(event.Agent, traceLead)+" as "+cmp.Or(spawned.Definition, "unnamed")+" on "+cmp.Or(spawned.Model, "the lead's model"), spawned.Ticket, oneLine(spawned.Mission))
		case session.EventAgentEnd:
			var ended session.AgentEndBody
			_ = json.Unmarshal(event.Body, &ended)
			row.Kind, row.State, row.Text = kindSpawn, ended.Status, traced("ended "+ended.Status, spent(ended.CostUSD))
			if ended.Status != subagent.Finished.String() {
				row.Kind, row.Outcome = kindStop, traceStopped
				unfinished = append(unfinished, len(rows))
			}
		case session.EventReport:
			var report session.ReportBody
			_ = json.Unmarshal(event.Body, &report)
			row.Kind, row.State, row.Text = kindReport, report.State, report.State+": "+oneLine(report.Text)
		case session.EventCompaction:
			var compacted struct {
				Compaction *turn.Compaction `json:"compaction"`
				Fork       *turn.Fork       `json:"fork"`
			}
			_ = json.Unmarshal(event.Body, &compacted)
			row.Kind, row.Text = kindCompaction, "compacted"
			if compacted.Compaction != nil {
				row.Text += " " + strconv.Itoa(compacted.Compaction.TokensBefore) + " to " + strconv.Itoa(compacted.Compaction.TokensAfter) + " tokens"
			}
			if compacted.Fork != nil {
				row.Text = "forked at " + strconv.Itoa(compacted.Fork.TokensBefore) + " tokens, carried " + strconv.Itoa(compacted.Fork.TokensAfter)
			}
		case session.EventListChange:
			var change session.ListChangeBody
			_ = json.Unmarshal(event.Body, &change)
			row.Kind, row.Text = kindList, traced(change.Kind+" "+strconv.Itoa(len(change.Before))+" to "+plural(len(change.After), "message"), change.Why)
		case session.EventRead:
			var read session.Read
			_ = json.Unmarshal(event.Body, &read)
			row.Kind, row.Text, row.Outcome = kindRead, traced(read.Tool+" "+read.Source, read.Span, sized(read.Bytes), read.Error), traceProblem(false, read.Error)
		case session.EventNotice:
			var notice session.StopBody
			_ = json.Unmarshal(event.Body, &notice)
			row.Kind, row.Text = kindNotice, oneLine(notice.Text)
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
			case event.Agent != "":
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
				Outcome: traceProblem(false, exchange.Error), Text: traced(exchange.Model, took(exchange.DurationMS), oneLine(exchange.Error))})
		}
	}
	for _, cut := range cacheTrace(events, exchanges).Breaks {
		rows = append(rows, traceRow{At: at[cut.Request], Kind: kindCache, Agent: cut.Agent, ID: cut.Request,
			Text: "read " + strconv.Itoa(cut.Read) + " of " + strconv.Itoa(cut.Cached) + " cached, " + cut.Gap + " after " + cut.After + ", first difference: " + cut.Differs})
	}
	return rows
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
