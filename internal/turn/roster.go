package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/internal/llm"
	"tofu/internal/session"
	"tofu/internal/subagent"
)

const listChangeFork = "fork"

type recordedSession struct {
	id     string
	events []session.Event
	unread []session.AgentRun
}

type recordedSubAgent struct {
	agent       subagent.SubAgent
	in          string
	call        string
	byLead      bool
	depth       int
	state       subagent.State
	ended       bool
	steps       int
	calls       []string
	active      time.Time
	report      string
	reportedAt  time.Time
	undelivered bool
	recent      bool
	fromHeader  bool
}

func RestoreSubAgents(store *session.Store, id string, roster *subagent.Roster, inbox *Inbox) error {
	lineage, err := store.Lineage(id)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	runs := recordedSubAgents(sessionChain(lineage))
	stopErr := endStoppedByTheRestart(store, runs)
	roster.RecallWith(spawnedIn(store, lineage))
	for _, run := range runs {
		agentID, restored := run.agent.ID, run.agent
		restored.State, restored.Report = run.state, run.report
		if run.fromHeader {
			roster.RestoreFromHeader(restored)
		} else {
			roster.Restore(restored)
		}
		roster.Stepped(agentID, run.steps, run.active, run.calls...)
		if !run.byLead {
			continue
		}
		if run.undelivered && run.recent {
			inbox.reportAgain(run.report)
		}
		inbox.adopt(restoredHeld(run.agent, run.depth, readBack(store, id, agentID)))
	}
	return stopErr
}

func readBack(store *session.Store, id, agentID string) func() ([]llm.Message, error) {
	return func() ([]llm.Message, error) {
		once := store.ReadOnce()
		lineage, err := once.LineageNaming(id, agentID)
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return subAgentHistory(once, sessionChain(lineage), agentID)
	}
}

func endStoppedByTheRestart(store *session.Store, runs []*recordedSubAgent) error {
	var failed []error
	for _, run := range runs {
		if run.ended {
			continue
		}
		log, err := store.Open(session.Header{ID: run.in})
		if errors.As(err, &session.BusyError{}) {
			continue
		}
		if err != nil {
			failed = append(failed, err)
			continue
		}
		store.Unread(run.in)
		trace := spawnTrace{site: spawnSite{log: log}}
		if !run.recent {
			failed = append(failed, trace.end(run.agent.ID, subagent.Parked), log.Close())
			run.state = subagent.Parked
			continue
		}
		stopped := "sub-agent " + run.agent.ID + " is " + subagent.Errored.String() + ": tofu closed it at step " + strconv.Itoa(run.steps) + " of " + strconv.Quote(run.agent.Mission)
		if len(run.calls) > 0 {
			stopped += ", after " + run.calls[len(run.calls)-1]
		}
		stopped += "; message it to resume."
		_, err = log.Append(session.Event{Agent: run.agent.ID, Kind: session.EventReport}, session.ReportBody{State: subagent.Errored.String(), Text: stopped})
		failed = append(failed, err, trace.end(run.agent.ID, subagent.Errored), log.Close())
		run.state, run.report, run.undelivered = subagent.Errored, stopped, true
	}
	return errors.Join(failed...)
}

func spawnedIn(store *session.Store, lineage []session.Generation) func() []subagent.SubAgent {
	return func() []subagent.SubAgent {
		var spawned []subagent.SubAgent
		for _, generation := range lineage {
			if !generation.Unread {
				continue
			}
			events, _ := store.Spawns(generation.Header.ID)
			for _, event := range events {
				var body session.SpawnBody
				if json.Unmarshal(event.Body, &body) == nil {
					spawned = append(spawned, subagent.SubAgent{ID: body.Agent, Mission: body.Mission, Owns: body.Owns, Ticket: body.Ticket})
				}
			}
		}
		return spawned
	}
}

func (t *SpawnTool) recall(id string) error {
	if !t.roster.Partial(id) || t.base.Sessions == nil {
		return nil
	}
	lineage, err := t.base.Sessions.ReadOnce().LineageNaming(t.base.Session, id)
	if err != nil {
		return fmt.Errorf("%s's record did not read back: %w", id, err)
	}
	runs := recordedSubAgents(sessionChain(lineage))
	at := slices.IndexFunc(runs, func(run *recordedSubAgent) bool { return run.agent.ID == id })
	if at < 0 {
		return nil
	}
	recorded := runs[at].agent
	t.roster.Recall(recorded)
	if held, _ := t.Inbox.find(id); held != nil {
		held.agent.Ticket, held.agent.Mission, held.agent.Brief, held.agent.Owns, held.agent.Regranted = recorded.Ticket, recorded.Mission, recorded.Brief, recorded.Owns, recorded.Regranted
		held.boundary = subagent.NewBoundary(id, "", recorded.Owns)
		held.trace.mission, held.trace.owns, held.trace.ticket = recorded.Mission, recorded.Owns, recorded.Ticket
	}
	return nil
}

func restoredHeld(agent subagent.SubAgent, depth int, reload func() ([]llm.Message, error)) *heldSubAgent {
	return &heldSubAgent{agent: agent, restored: true, reload: reload, inbox: NewInbox(),
		boundary: subagent.NewBoundary(agent.ID, "", agent.Owns),
		trace:    spawnTrace{definition: agent.Agent, model: agent.Model, mission: agent.Mission, owns: agent.Owns, ticket: agent.Ticket, depth: depth}}
}

func recordedSubAgents(chain []recordedSession) []*recordedSubAgent {
	var order []*recordedSubAgent
	ran, briefs, regrants := map[string]*recordedSubAgent{}, map[string]string{}, map[string]messageArgs{}
	var leadIdle, lastTurnFrom time.Time
	for _, recorded := range chain {
		for _, run := range recorded.unread {
			if ran[run.Agent] != nil {
				continue
			}
			state, _ := endedAs(run.Status)
			ran[run.Agent] = &recordedSubAgent{in: recorded.id, call: run.SpawnCall, byLead: run.ParentAgent == "", depth: run.Depth, state: state, ended: true, fromHeader: true, active: run.StartedAt,
				agent: subagent.SubAgent{ID: run.Agent, Agent: run.Definition, Model: run.Model, Started: run.StartedAt}}
			if run.EndedAt != nil {
				ran[run.Agent].active = *run.EndedAt
			}
			order = append(order, ran[run.Agent])
		}
		var leadReports []string
		reportEvents := false
		for _, event := range recorded.events {
			run := ran[event.Agent]
			switch {
			case event.Kind == session.EventSpawn:
				var body session.SpawnBody
				if json.Unmarshal(event.Body, &body) != nil || ran[body.Agent] != nil {
					continue
				}
				ran[body.Agent] = &recordedSubAgent{in: recorded.id, call: event.Call, byLead: event.Agent == "", depth: body.Depth, state: subagent.Parked, active: event.At,
					agent: subagent.SubAgent{ID: body.Agent, Agent: body.Definition, Model: body.Model, Mission: body.Mission, Owns: body.Owns, Ticket: body.Ticket, Started: event.At}}
				order = append(order, ran[body.Agent])
			case event.Kind == session.EventToolCall:
				var call session.CallBody
				_ = json.Unmarshal(event.Body, &call)
				var args spawnArgs
				if event.Agent == "" && call.Tool == "spawn" && json.Unmarshal(call.Args, &args) == nil {
					briefs[event.Call] = args.Task
				}
				var message messageArgs
				if call.Tool == (messageTool{}).Name() && json.Unmarshal(call.Args, &message) == nil && slices.Contains([]string{doGrant, doRevoke, doReplace}, message.Do) {
					regrants[event.Call] = message
				}
				if run != nil {
					run.calls = append(run.calls, call.Tool)
				}
			case event.Kind == session.EventToolResult && regrants[event.Call].Do != "":
				message := regrants[event.Call]
				var result session.ResultBody
				if granted := ran[message.To]; granted != nil && json.Unmarshal(event.Body, &result) == nil && result.ToolOutcome == session.ToolOutcomeRan {
					granted.agent.Owns = ownsAfter(message.Do, granted.agent.Owns, message.Owns)
					granted.agent.Regranted = append(granted.agent.Regranted, subagent.OwnsChange{At: event.At, Did: message.Do, Paths: message.Owns})
				}
			case event.Kind == session.EventMessage && event.Agent == "":
				var message session.MessageBody
				if json.Unmarshal(event.Body, &message) != nil {
					continue
				}
				if strings.Contains(message.Origin, sourceReport) {
					leadReports = append(leadReports, message.Content)
				}
				for _, told := range order {
					told.undelivered = told.undelivered && (message.Role != llm.RoleUser.String() || !event.At.After(told.reportedAt))
				}
			case event.Agent == "" && event.Kind == session.EventTurnEnd:
				leadIdle = event.At
			case event.Agent == "" && event.Kind == session.EventTurnStart:
				lastTurnFrom = leadIdle
			case run == nil:
			case event.Kind == session.EventReport:
				var report session.ReportBody
				_ = json.Unmarshal(event.Body, &report)
				run.report, run.reportedAt, run.undelivered, reportEvents = report.Text, event.At, report.Text != "", true
			case event.Kind == session.EventAgentEnd:
				var ended session.AgentEndBody
				_ = json.Unmarshal(event.Body, &ended)
				run.state, run.ended = endedAs(ended.Status)
				run.active = event.At
			case event.Kind == session.EventRequest && event.Attempt <= session.FirstAttempt:
				run.steps, run.active, run.ended = run.steps+1, event.At, false
			}
		}
		if !reportEvents {
			reportsWrittenBeforeReportEvents(leadReports, order)
		}
	}
	for _, run := range order {
		run.agent.Brief, run.recent = briefs[run.call], !run.active.Before(lastTurnFrom)
	}
	return order
}

func (t *SpawnTool) recompose(held *heldSubAgent) error {
	if _, err := held.conversation(); err != nil {
		return fmt.Errorf("%s's conversation did not read back: %w", held.agent.ID, err)
	}
	if !held.restored {
		return nil
	}
	definition, err := t.SubAgents.Named(held.agent.Agent)
	if err != nil {
		return err
	}
	owns := held.boundary.Owns()
	system, environment, err := t.SubAgents.prompt(t.base, definition, held.agent.Brief, owns)
	if err != nil {
		return err
	}
	scratch, err := t.scratch(context.Background(), held.agent.ID)
	if err != nil {
		return fmt.Errorf("%s's scratch folder was not made: %w", held.agent.ID, err)
	}
	held.definition, held.system, held.environment, held.restored = definition, system, environment+scratchWords(scratch), false
	held.boundary, held.scratch = subagent.NewBoundary(held.agent.ID, scratch.Dir(), owns), scratch
	return nil
}

func sessionChain(lineage []session.Generation) []recordedSession {
	chain := make([]recordedSession, len(lineage))
	for i, generation := range lineage {
		chain[i] = recordedSession{id: generation.Header.ID, events: generation.Events}
		if generation.Unread {
			chain[i].unread = generation.Header.Agents
		}
	}
	return chain
}

func endedAs(status string) (subagent.State, bool) {
	for _, state := range []subagent.State{subagent.Finished, subagent.Errored, subagent.Parked} {
		if state.String() == status {
			return state, true
		}
	}
	return subagent.Parked, false
}

func reportsWrittenBeforeReportEvents(leadReports []string, order []*recordedSubAgent) {
	for _, message := range leadReports {
		var current *recordedSubAgent
		for _, paragraph := range strings.Split(message, "\n\n") {
			for _, run := range order {
				if (strings.HasPrefix(paragraph, "sub-agent "+run.agent.ID+" is ") || strings.HasPrefix(paragraph, run.agent.ID+" ran")) && run != current {
					run.report, current = "", run
				}
			}
			if current != nil {
				current.report = strings.TrimPrefix(current.report+"\n\n"+paragraph, "\n\n")
			}
		}
	}
}

func subAgentHistory(store *session.Store, chain []recordedSession, agentID string) ([]llm.Message, error) {
	var since []session.Event
	var change session.ListChangeBody
	changedIn := ""
	for _, recorded := range chain {
		part, err := store.Part(recorded.events, agentID)
		if err != nil {
			return nil, err
		}
		since = append(since, part...)
		for i, event := range part {
			var changed session.ListChangeBody
			if event.Kind == session.EventListChange && json.Unmarshal(event.Body, &changed) == nil {
				since, change, changedIn = part[i+1:], changed, recorded.id
			}
		}
	}
	var conversation []llm.Message
	if changedIn != "" && change.Kind != listChangeFork {
		blobs, err := store.Blobs(changedIn)
		if err != nil {
			return nil, err
		}
		for _, hash := range change.After {
			var row MessageRow
			if err := json.Unmarshal(blobs[hash], &row); err != nil {
				return nil, fmt.Errorf("the message %s carried into %s's %s is not in session %s: %w", hash, agentID, change.Kind, changedIn, err)
			}
			message, err := messageOf(row)
			if err != nil {
				return nil, err
			}
			if message.Role != llm.RoleSystem {
				conversation = append(conversation, message)
			}
		}
	}
	after, err := ConversationFrom(since)
	return resumable(append(conversation, after...)), err
}

func resumable(messages []llm.Message) []llm.Message {
	stripped := slices.Clone(messages)
	for i := range stripped {
		stripped[i].Thinking = llm.Thinking{}
	}
	return Sendable(stripped)
}
