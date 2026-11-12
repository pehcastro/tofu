package loop

import (
	"errors"
	"slices"
	"time"

	"tofu/internal/agent/subagents"
	"tofu/internal/models/llm/core"
	"tofu/internal/store/session"
)

type spawnSiteKey struct{}

type spawnSite struct {
	log          *session.Log
	turn         string
	agent        string
	call         string
	conversation []core.Message
}

func (r *record) site(call string, conversation []core.Message) spawnSite {
	sofar := conversation[:len(conversation):len(conversation)]
	if r == nil {
		return spawnSite{call: call, conversation: sofar}
	}
	return spawnSite{log: r.log, turn: r.turn, agent: r.agent, call: call, conversation: sofar}
}

type spawnTrace struct {
	site       spawnSite
	definition string
	model      string
	mission    string
	owns       []string
	ticket     string
	depth      int
	stagger    stagger
	estimate   *openEstimate
}

func (t *SpawnTool) estimateStart(site spawnSite, agent, model, task string) (*openEstimate, error) {
	at := time.Now()
	body, err := t.base.estimate(estimateFor{scope: session.EstimateAgent, agent: agent, model: model, task: task})
	if err != nil || site.log == nil {
		return &openEstimate{at: at, body: body}, err
	}
	event, err := site.log.Append(session.Event{At: at, Turn: site.turn, Agent: site.agent, Call: site.call, Kind: session.EventEstimate}, body)
	return &openEstimate{id: event.ID, at: at, body: body}, err
}

func (s spawnTrace) settle(id string, outcome session.EstimateOutcome, began time.Time, ran []Row) error {
	if s.estimate == nil {
		return nil
	}
	split := splitOf(ran)
	split.QueueMS = began.Sub(s.estimate.at).Milliseconds()
	return s.estimate.settle(s.site.log, session.Event{Turn: s.site.turn, Agent: id}, outcome, time.Since(s.estimate.at), split)
}

func (s spawnTrace) begin(id string) error {
	log, site := s.site.log, s.site
	if log == nil {
		return nil
	}
	if slices.ContainsFunc(log.Header().Agents, func(run session.AgentRun) bool { return run.Agent == id }) {
		return markRun(log, id, subagents.Working, nil)
	}
	_, err := log.Append(session.Event{Turn: site.turn, Agent: site.agent, Call: site.call, Kind: session.EventSpawn},
		session.SpawnBody{Agent: id, Definition: s.definition, Model: s.model, Mission: s.mission, Owns: s.owns, Ticket: s.ticket, Depth: s.depth,
			Stagger: s.stagger.decided, StaggerWhy: s.stagger.why})
	return errors.Join(err, log.Edit(func(header *session.Header) {
		header.Agents = append(header.Agents, session.AgentRun{Agent: id, Definition: s.definition, Model: s.model, ParentAgent: site.agent,
			SpawnCall: site.call, SpawnTurn: site.turn, Depth: s.depth, Status: subagents.Working.String(), StartedAt: time.Now()})
	}))
}

func (s spawnTrace) end(id string, state subagents.State) error {
	log := s.site.log
	if log == nil {
		return nil
	}
	ended := session.AgentEndBody{Status: state.String()}
	for _, run := range log.Header().Agents {
		if run.Agent == id {
			ended.Usage, ended.CostUSD = run.Usage, run.CostUSD
		}
	}
	_, err := log.Append(session.Event{Turn: s.site.turn, Agent: id, Kind: session.EventAgentEnd}, ended)
	at := time.Now()
	return errors.Join(err, markRun(log, id, state, &at))
}

func markRun(log *session.Log, id string, state subagents.State, ended *time.Time) error {
	return log.Edit(func(header *session.Header) {
		for i := range header.Agents {
			if header.Agents[i].Agent == id {
				header.Agents[i].Status, header.Agents[i].EndedAt = state.String(), ended
			}
		}
	})
}
