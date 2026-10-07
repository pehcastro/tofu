package turn

import (
	"encoding/json"
	"errors"
	"io/fs"
	"slices"
	"strings"
	"time"

	"tofu/internal/llm"
	"tofu/internal/session"
	"tofu/internal/subagent"
)

type recordedSession struct {
	id     string
	events []session.Event
}

type recordedSubAgent struct {
	agent  subagent.SubAgent
	call   string
	byLead bool
	depth  int
	state  subagent.State
	steps  int
	calls  []string
	active time.Time
	report string
}

func RestoreSubAgents(store *session.Store, id string, roster *subagent.Roster, inbox *Inbox) error {
	chain, err := sessionChain(store, id)
	if err != nil {
		return err
	}
	var failed []error
	for _, run := range recordedSubAgents(chain) {
		agentID := run.agent.ID
		if err := roster.Hold(run.agent); err != nil {
			failed = append(failed, err)
			continue
		}
		roster.Reached(agentID, run.state, run.report)
		roster.Stepped(agentID, run.steps, run.active, run.calls...)
		if !run.byLead {
			continue
		}
		history, err := subAgentHistory(store, chain, agentID)
		if err != nil {
			failed = append(failed, err)
			continue
		}
		inbox.mu.Lock()
		inbox.held[agentID] = &heldSubAgent{agent: run.agent, history: history, restored: true, inbox: NewInbox(),
			boundary: &subagent.Boundary{Ticket: agentID, Owns: run.agent.Owns},
			trace:    spawnTrace{definition: run.agent.Agent, model: run.agent.Model, mission: run.agent.Mission, owns: run.agent.Owns, depth: run.depth}}
		inbox.mu.Unlock()
	}
	return errors.Join(failed...)
}

func recordedSubAgents(chain []recordedSession) []*recordedSubAgent {
	var order []*recordedSubAgent
	ran, briefs := map[string]*recordedSubAgent{}, map[string]string{}
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
				ran[body.Agent] = &recordedSubAgent{call: event.Call, byLead: event.Agent == "", depth: body.Depth, state: subagent.Parked, active: event.At,
					agent: subagent.SubAgent{ID: body.Agent, Agent: body.Definition, Model: body.Model, Mission: body.Mission, Owns: body.Owns, Started: event.At}}
				order = append(order, ran[body.Agent])
			case event.Kind == session.EventToolCall:
				var call session.CallBody
				_ = json.Unmarshal(event.Body, &call)
				var args spawnArgs
				if event.Agent == "" && call.Tool == "spawn" && json.Unmarshal(call.Args, &args) == nil {
					briefs[event.Call] = args.Task
				}
				if run != nil {
					run.calls = append(run.calls, call.Tool)
				}
			case event.Kind == session.EventMessage && event.Agent == "":
				var message session.MessageBody
				if json.Unmarshal(event.Body, &message) == nil && strings.Contains(message.Origin, sourceReport) {
					leadReports = append(leadReports, message.Content)
				}
			case run == nil:
			case event.Kind == session.EventReport:
				var report session.ReportBody
				_ = json.Unmarshal(event.Body, &report)
				run.report, reportEvents = report.Text, true
			case event.Kind == session.EventAgentEnd:
				var ended session.AgentEndBody
				_ = json.Unmarshal(event.Body, &ended)
				run.state, run.active = stateAfterResume(ended.Status), event.At
			case event.Kind == session.EventRequest && event.Attempt <= session.FirstAttempt:
				run.steps, run.active = run.steps+1, event.At
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
	system, environment, err := t.SubAgents.prompt(t.base, definition, held.agent.Brief, held.agent.Owns)
	if err != nil {
		return err
	}
	held.definition, held.system, held.environment, held.restored = definition, system, environment, false
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

func stateAfterResume(status string) subagent.State {
	for _, state := range []subagent.State{subagent.Finished, subagent.Errored} {
		if state.String() == status {
			return state
		}
	}
	return subagent.Parked
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
	var events []session.Event
	for _, recorded := range chain {
		part, err := store.Part(recorded.events, agentID)
		if err != nil {
			return nil, err
		}
		events = append(events, part...)
	}
	conversation, err := ConversationFrom(events)
	return resumable(conversation), err
}
