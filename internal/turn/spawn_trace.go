package turn

import (
	"context"
	"errors"
	"slices"
	"time"

	"tofu/internal/llm"
	"tofu/internal/session"
	"tofu/internal/subagent"
)

type spawnSiteKey struct{}

type spawnSite struct {
	log          *session.Log
	turn         string
	agent        string
	call         string
	conversation []llm.Message
}

func (r *record) site(call string, conversation []llm.Message) spawnSite {
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
}

type subAgentKey struct{}

func SubAgentAsking(ctx context.Context) string {
	id, _ := ctx.Value(subAgentKey{}).(string)
	return id
}

func (s spawnTrace) begin(id string) error {
	log, site := s.site.log, s.site
	if log == nil {
		return nil
	}
	if slices.ContainsFunc(log.Header().Agents, func(run session.AgentRun) bool { return run.Agent == id }) {
		return markRun(log, id, subagent.Working, nil)
	}
	_, err := log.Append(session.Event{Turn: site.turn, Agent: site.agent, Call: site.call, Kind: session.EventSpawn},
		session.SpawnBody{Agent: id, Definition: s.definition, Model: s.model, Mission: s.mission, Owns: s.owns, Ticket: s.ticket, Depth: s.depth})
	return errors.Join(err, log.Edit(func(header *session.Header) {
		header.Agents = append(header.Agents, session.AgentRun{Agent: id, Definition: s.definition, Model: s.model, ParentAgent: site.agent,
			SpawnCall: site.call, SpawnTurn: site.turn, Depth: s.depth, Status: subagent.Working.String(), StartedAt: time.Now()})
	}))
}

func (s spawnTrace) end(id string, state subagent.State) error {
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

func markRun(log *session.Log, id string, state subagent.State, ended *time.Time) error {
	return log.Edit(func(header *session.Header) {
		for i := range header.Agents {
			if header.Agents[i].Agent == id {
				header.Agents[i].Status, header.Agents[i].EndedAt = state.String(), ended
			}
		}
	})
}
