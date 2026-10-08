package host

import (
	"cmp"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"tofu/internal/session"
	roster "tofu/internal/subagent"
)

const (
	pointToolGate     = "tool_gate"
	pointRuleOverride = "rule_override"
	hunkHeader        = "@@ -%d,%d +%d,%d @@"
)

type openTool struct {
	tool string
	path string
	at   time.Time
}

type items struct {
	session string
	turn    string
	task    string
	began   time.Time
	context *ContextUse
	status  Status
	message string
	written string
	minted  int
	tools   map[string]openTool
	agents  map[string]SubAgentRow
}

func newItems(session string) items {
	return items{session: session, tools: map[string]openTool{}, agents: map[string]SubAgentRow{}}
}

func (s *items) all(chat []Event) []outgoing {
	var lines []outgoing
	for _, event := range chat {
		lines = append(lines, s.translate(event, time.Now())...)
	}
	return lines
}

func (s *items) identity(agent, item string) Identity {
	return Identity{Session: s.session, Turn: s.turn, Agent: agent, Item: item}
}

func (s *items) mint(kind string) string {
	s.minted++
	return session.EventIDFor(s.session+s.turn, kind+" "+strconv.Itoa(s.minted))
}

func (s *items) translate(event Event, now time.Time) []outgoing {
	var out []outgoing
	if s.message != "" && event.Kind != EventStreamReset && endsTheLeadReply(event) {
		out = append(out, notify("message.completed", &Text{Identity: s.identity("", s.message), Text: s.written}))
		s.message, s.written = "", ""
	}
	id := s.identity(event.Agent, event.ID)
	switch event.Kind {
	case EventTurnStarted:
		s.turn, s.task, s.began, s.status = event.ID, event.Text, now, StatusFailed
		return append(out, kept("turn.started", &TurnStarted{Identity: s.identity("", s.turn), Task: event.Text, Origin: event.Origin, StartedAt: now}))
	case EventTurnEnded:
		return append(out, kept("turn.completed", &TurnCompleted{Identity: s.identity("", s.turn), Status: s.status, StartedAt: s.began, WorkedForMs: now.Sub(s.began).Milliseconds()}))
	case EventDone:
		s.status = event.Status
		return append(out, s.agentChanges(event.SubAgents)...)
	case EventSubAgent:
		return append(out, s.agentChanges(event.SubAgents)...)
	case EventTextDelta:
		if s.message == "" {
			s.message = s.mint("message")
			out = append(out, notify("message.started", &Marker{Identity: s.identity("", s.message)}))
		}
		s.written += event.Text
		return append(out, notify("message.delta", &Text{Identity: s.identity("", s.message), Text: event.Text}))
	case EventText:
		id.Item = s.mint("message")
		return append(out, notify("message.started", &Marker{Identity: id}), notify("message.completed", &Text{Identity: id, Text: event.Text}))
	case EventTask:
		id.Item = s.mint("task")
		return append(out, notify("message.user", &UserMessage{Identity: id, Text: event.Text, Origin: event.Origin}))
	case EventStreamReset:
		if event.Agent != "" || s.message == "" {
			return out
		}
		reset := notify("message.reset", &Marker{Identity: s.identity("", s.message)})
		s.message, s.written = "", ""
		return append(out, reset)
	case EventThinking:
		return append(out, notify("thinking.delta", &Text{Identity: id, Text: event.Text}))
	case EventSteered:
		id.Item = s.mint("steered")
		return append(out, notify("turn.steered", &Text{Identity: id, Text: event.Text}))
	case EventToolCall:
		var args struct {
			Path string `json:"path"`
		}
		_ = json.Unmarshal(event.Args, &args)
		s.tools[event.ID] = openTool{tool: event.Tool, path: args.Path, at: now}
		return append(out, notify("tool.started", &ToolStarted{Identity: id, Tool: event.Tool, Args: wholeJSON(event.Args), StartedAt: now}))
	case EventToolResult:
		return append(out, s.toolCompleted(id, event, now)...)
	case EventDecision:
		if event.Decision == nil {
			return out
		}
		id.Item = cmp.Or(event.ID, s.mint("decision"))
		point := pointToolGate
		if event.Decision.OverridesRule != "" {
			point = pointRuleOverride
		}
		return append(out, kept("decision", &DecisionMade{Identity: id, Judgement: judgementOf(*event.Decision), Point: point, Tool: event.Decision.Tool, OverridesRule: event.Decision.OverridesRule}))
	case EventNote:
		return append(out, notify("note", s.said(id, event, SaidNote)))
	case EventGateOff:
		return append(out, notify("note", s.said(id, event, SaidGateOff)))
	case EventFailure:
		return append(out, kept("failure", s.said(id, event, SaidFailure)))
	case EventStats:
		id.Item = session.EventIDFor(s.turn, "usage "+event.Agent)
		return append(out, merged("usage.updated", event.Agent, &UsageUpdated{Identity: id, Model: event.Model, TokensIn: event.TokensIn, TokensOut: event.TokensOut, CacheRead: event.CacheRead, Decisions: event.Decisions}))
	case EventContext:
		id.Item = session.EventIDFor(s.turn, "context")
		s.context = &ContextUse{Used: event.Context.Used, Budget: event.Context.Budget}
		return append(out, merged("context.updated", "", &ContextUpdated{Identity: id, ContextUse: *s.context}))
	case EventPlan:
		id.Item = session.EventIDFor(s.turn, "plan")
		steps := make([]PlanStep, 0, len(event.Plan))
		for _, item := range event.Plan {
			steps = append(steps, PlanStep{Phase: item.Phase, Text: item.Text, State: stepState(item.State)})
		}
		return append(out, merged("plan.updated", "", &PlanUpdated{Identity: id, Items: steps}))
	case EventSession:
		s.session = event.ID
		return append(out, sessionUpdated(s.identity("", event.ID), event))
	case EventForkEnd:
		if event.Fork == nil {
			return out
		}
		s.session = event.Fork.To
		forked := *event.Fork
		return append(out, kept("session.forked", &SessionForked{Identity: s.identity("", forked.To), From: forked.From, To: forked.To, Kind: forked.Kind, Before: forked.Before, After: forked.After}))
	case EventAwaitPerson:
		asked := approvalRequest(id, event)
		request := kept(ApprovalMethod, &asked)
		request.msg.ID, _ = json.Marshal(event.ID)
		return append(out, request)
	case EventPersisted:
		if event.Logged.Call != "" {
			id.Item = session.EventIDFor(cmp.Or(event.Agent, s.turn), event.Logged.Call)
		}
		return append(out, notify("item.persisted", &Persisted{Identity: id, LogSeq: event.Logged.Seq, LogID: event.ID, Kind: string(event.Logged.Kind)}))
	case EventForkStart, EventResumed, EventRequesting:
		return out
	}
	panic("host: unknown event kind " + strconv.Itoa(int(event.Kind)))
}

func (s *items) said(id Identity, event Event, kind SaidKind) *Said {
	id.Item = cmp.Or(event.ID, s.mint("said"))
	return &Said{Identity: id, Kind: kind, Text: event.Text}
}

func sessionUpdated(id Identity, event Event) outgoing {
	updated := &SessionUpdated{Identity: id, Name: event.Text, Root: event.Root, LastAt: event.LastAt}
	if family := event.Identity; family != nil {
		updated.Name, updated.Tag, updated.Generation, updated.Handle, updated.Started = family.Name, family.Tag, family.Generation, family.Handle(), family.Started
	}
	return kept("session.updated", updated)
}

func approvalRequest(id Identity, event Event) ApprovalRequest {
	asked := ApprovalRequest{Identity: id, Approval: event.ID, Tool: event.Tool, Target: event.Text, Args: wholeJSON(event.Args)}
	if event.Decision != nil {
		judged := judgementOf(*event.Decision)
		asked.Judged = &judged
	}
	return asked
}

func wholeJSON(raw json.RawMessage) json.RawMessage {
	if json.Valid(raw) {
		return raw
	}
	quoted, _ := json.Marshal(string(raw))
	return quoted
}

func (s *items) toolCompleted(id Identity, event Event, now time.Time) []outgoing {
	opened, known := s.tools[event.ID]
	delete(s.tools, event.ID)
	completed := &ToolCompleted{Identity: id, Tool: opened.tool, Failed: event.Failed, ExitCode: event.ExitCode, Bytes: event.Bytes, Lines: lineCount(event.Detail), Output: event.Detail}
	if known {
		completed.DurationMs = now.Sub(opened.at).Milliseconds()
	}
	out := []outgoing{notify("tool.completed", completed)}
	switch {
	case event.Created != "":
		return append(out, notify("file.edit", &FileEdit{Identity: id, Path: opened.path, Op: EditCreate, Hunks: []Hunk{createdHunk(event.Created)}}))
	case event.Diff != "":
		return append(out, notify("file.edit", &FileEdit{Identity: id, Path: opened.path, Op: EditModify, Hunks: hunksOf(event.Diff)}))
	}
	return out
}

func lineCount(text string) int {
	trimmed := strings.TrimRight(text, "\n")
	if trimmed == "" {
		return 0
	}
	return strings.Count(trimmed, "\n") + 1
}

func createdHunk(content string) Hunk {
	hunk := Hunk{NewStart: 1}
	for _, line := range strings.SplitAfter(strings.TrimSuffix(content, "\n"), "\n") {
		hunk.Lines = append(hunk.Lines, HunkLine{Kind: LineAdded, Text: strings.TrimSuffix(line, "\n")})
	}
	hunk.NewLines = len(hunk.Lines)
	return hunk
}

func hunksOf(diff string) []Hunk {
	var hunks []Hunk
	kinds := map[byte]LineKind{' ': LineContext, '-': LineRemoved, '+': LineAdded}
	for _, line := range strings.Split(strings.TrimSuffix(diff, "\n"), "\n") {
		var hunk Hunk
		if _, err := fmt.Sscanf(line, hunkHeader, &hunk.OldStart, &hunk.OldLines, &hunk.NewStart, &hunk.NewLines); err == nil {
			hunks = append(hunks, hunk)
			continue
		}
		if line == "" || len(hunks) == 0 {
			continue
		}
		if kind, known := kinds[line[0]]; known {
			last := &hunks[len(hunks)-1]
			last.Lines = append(last.Lines, HunkLine{Kind: kind, Text: line[1:]})
		}
	}
	return hunks
}

func judgementOf(decision Decision) Judgement {
	judged := Judgement{Verdict: VerdictName(decision.Verdict.String()), Answers: decision.Answers, Failure: decision.Failure, Enforced: decision.Enforced}
	if decision.Reason.Question != "" {
		reason := decision.Reason
		judged.Reason = &reason
	}
	return judged
}

func stepState(state PlanState) StepState {
	switch state {
	case PlanPending:
		return StepPending
	case PlanRunning:
		return StepRunning
	case PlanDone:
		return StepDone
	case PlanDropped:
		return StepDropped
	}
	panic("host: unknown plan state " + strconv.Itoa(int(state)))
}

func (s *items) agentChanges(rows []SubAgentRow) []outgoing {
	var out []outgoing
	for _, row := range rows {
		was, known := s.agents[row.Name]
		s.agents[row.Name] = row
		id := s.identity(row.Name, row.Name)
		state := AgentState(row.State.String())
		if !known {
			out = append(out, kept("agent.started", &AgentStarted{Identity: id, Instance: row.Name, Kind: row.Agent, Number: len(s.agents), Task: row.Doing, Owns: row.Owns, Model: row.Model, State: state, StartedAt: row.Started}))
		} else if changed := agentDelta(id, was, row); changed != nil {
			out = append(out, merged("agent.updated", row.Name, changed))
		}
		if ended(row.State) && (!known || !ended(was.State)) {
			out = append(out, kept("agent.ended", &AgentEnded{Identity: id, Instance: row.Name, State: state, Report: row.Report}))
		}
	}
	return out
}

func agentDelta(id Identity, was, now SubAgentRow) *AgentUpdated {
	unchanged := AgentUpdated{Identity: id, Instance: now.Name}
	delta := unchanged
	if was.State != now.State {
		state := AgentState(now.State.String())
		delta.State = &state
	}
	if was.Doing != now.Doing {
		delta.Task = &now.Doing
	}
	if was.Model != now.Model {
		delta.Model = &now.Model
	}
	if was.Steps != now.Steps {
		delta.Steps = &now.Steps
	}
	if was.Tokens != now.Tokens {
		delta.Tokens = &now.Tokens
	}
	if was.Report != now.Report {
		delta.Report = &now.Report
	}
	if delta == unchanged {
		return nil
	}
	return &delta
}

func ended(state roster.State) bool {
	switch state {
	case roster.Errored, roster.Finished:
		return true
	case roster.Working, roster.WaitingAnswer, roster.InReview, roster.Reopened, roster.Parked:
		return false
	}
	panic("host: unknown sub-agent state " + state.String())
}
