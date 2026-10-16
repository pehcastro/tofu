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
}

func RestoreSubAgents(store *session.Store, id string, roster *subagent.Roster, inbox *Inbox) error {
	chain, err := sessionChain(store, id)
	if err != nil {
		return err
	}
	runs := recordedSubAgents(chain)
	stopErr := endStoppedByTheRestart(store, runs)
	store.ForgetRead()
	unread := *store
	for _, run := range runs {
		agentID, restored := run.agent.ID, run.agent
		restored.State, restored.Report = run.state, run.report
		roster.Restore(restored)
		roster.Stepped(agentID, run.steps, run.active, run.calls...)
		if !run.byLead {
			continue
		}
		if run.undelivered {
			inbox.reportAgain(run.report)
		}
		inbox.adopt(restoredHeld(run.agent, run.depth, func() ([]llm.Message, error) {
			chain, err := sessionChain(&unread, id)
			if err != nil {
				return nil, err
			}
			return subAgentHistory(&unread, chain, agentID)
		}))
	}
	return stopErr
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
		stopped := "sub-agent " + run.agent.ID + " is " + subagent.Errored.String() + ": tofu closed while it was working, at step " + strconv.Itoa(run.steps)
		if len(run.calls) > 0 {
			stopped += " after calling " + run.calls[len(run.calls)-1]
		}
		stopped += ", and it did not finish. Message it to resume from its conversation, or spawn the work again."
		_, err = log.Append(session.Event{Agent: run.agent.ID, Kind: session.EventReport}, session.ReportBody{State: subagent.Errored.String(), Text: stopped})
		failed = append(failed, err, spawnTrace{site: spawnSite{log: log}}.end(run.agent.ID, subagent.Errored), log.Close())
		run.state, run.report, run.undelivered = subagent.Errored, stopped, true
	}
	return errors.Join(failed...)
}

func restoredHeld(agent subagent.SubAgent, depth int, reload func() ([]llm.Message, error)) *heldSubAgent {
	return &heldSubAgent{agent: agent, restored: true, reload: reload, inbox: NewInbox(),
		boundary: subagent.NewBoundary(agent.ID, "", agent.Owns),
		trace:    spawnTrace{definition: agent.Agent, model: agent.Model, mission: agent.Mission, owns: agent.Owns, depth: depth}}
}

func recordedSubAgents(chain []recordedSession) []*recordedSubAgent {
	var order []*recordedSubAgent
	ran, briefs, regrants := map[string]*recordedSubAgent{}, map[string]string{}, map[string]messageArgs{}
	for _, recorded := range chain {
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
		run.agent.Brief = briefs[run.call]
	}
	return order
}

func (t *SpawnTool) recompose(held *heldSubAgent) error {
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
	if _, err := held.conversation(); err != nil {
		return fmt.Errorf("%s's conversation did not read back: %w", held.agent.ID, err)
	}
	scratch, err := t.scratch(context.Background(), held.agent.ID)
	if err != nil {
		return fmt.Errorf("%s's scratch folder was not made: %w", held.agent.ID, err)
	}
	held.definition, held.system, held.environment, held.restored = definition, system, environment+scratchWords(scratch), false
	held.boundary, held.scratch = subagent.NewBoundary(held.agent.ID, scratch.Dir(), owns), scratch
	return nil
}

func sessionChain(store *session.Store, id string) ([]recordedSession, error) {
	var chain []recordedSession
	for at := id; at != "" && !slices.ContainsFunc(chain, func(seen recordedSession) bool { return seen.id == at }); {
		events, err := store.Events(at)
		if errors.Is(err, fs.ErrNotExist) {
			break
		}
		if err != nil {
			return nil, err
		}
		header, err := store.Header(at)
		if err != nil {
			return nil, err
		}
		chain = append([]recordedSession{{id: at, events: events}}, chain...)
		at = header.Parent
	}
	return chain, nil
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
