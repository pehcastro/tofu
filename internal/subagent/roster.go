package subagent

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"tofu/internal/konst"
)

type State int

const (
	Working State = iota
	WaitingAnswer
	InReview
	Reopened
	Parked
	Errored
	Finished
)

func States() []State {
	return []State{Working, WaitingAnswer, InReview, Reopened, Parked, Errored, Finished}
}

func (s State) String() string {
	switch s {
	case Working:
		return "working"
	case WaitingAnswer:
		return "waiting_answer"
	case InReview:
		return "in_review"
	case Reopened:
		return "reopened"
	case Parked:
		return "parked"
	case Errored:
		return "errored"
	case Finished:
		return "finished"
	}
	panic("subagent: unknown sub-agent state " + strconv.Itoa(int(s)))
}

func (s State) settled() bool {
	switch s {
	case Errored, Finished:
		return true
	case Working, WaitingAnswer, InReview, Reopened, Parked:
		return false
	}
	panic("subagent: unknown sub-agent state " + strconv.Itoa(int(s)))
}

type SubAgent struct {
	ID           string
	Agent        string
	Model        string
	Mission      string
	Brief        string
	Owns         []string
	State        State
	Report       string
	Round        int
	Started      time.Time
	Active       time.Time
	Steps        int
	Calling      []string
	CallsDropped int
}

type RoundCapError struct {
	Ticket string
	Cap    int
}

func (e RoundCapError) Error() string {
	name := e.Ticket
	if name == "" {
		name = "this sub-agent"
	}
	return fmt.Sprintf("reopen refused: %s already reached the sub-agent round cap of %d", name, e.Cap)
}

type ReopenReasonError struct{}

func (ReopenReasonError) Error() string {
	return "reopen refused: a reopen needs a reason, and none was given"
}

type CollisionError struct {
	SubAgent     string
	Glob         string
	Holder       string
	HolderGlob   string
	HolderReport string
}

func (e CollisionError) Error() string {
	return fmt.Sprintf("%s cannot hold %q: %s already holds %q and the two overlap", e.SubAgent, e.Glob, e.Holder, e.HolderGlob)
}

type Roster struct {
	held   sync.Mutex
	agents []SubAgent
}

func (r *Roster) Hold(agent SubAgent) error {
	r.held.Lock()
	defer r.held.Unlock()
	for _, glob := range agent.Owns {
		if err := validGlob(glob); err != nil {
			return err
		}
		for _, held := range r.agents {
			if held.State.settled() {
				continue
			}
			for _, other := range held.Owns {
				if overlap(glob, other) {
					return CollisionError{SubAgent: agent.ID, Glob: glob, Holder: held.ID, HolderGlob: other, HolderReport: held.Report}
				}
			}
		}
	}
	agent.State, agent.Active, agent.Round = Working, agent.Started, 1
	r.agents = append(r.agents, agent)
	return nil
}

func (r *Roster) NextID(definition string, recorded []string) string {
	prefix := cmp.Or(definition, "sub") + "-"
	r.held.Lock()
	defer r.held.Unlock()
	ids := slices.Clone(recorded)
	for _, agent := range r.agents {
		ids = append(ids, agent.ID)
	}
	last := 0
	for _, id := range ids {
		if n, err := strconv.Atoi(strings.TrimPrefix(id, prefix)); err == nil && strings.HasPrefix(id, prefix) {
			last = max(last, n)
		}
	}
	return prefix + strconv.Itoa(last+1)
}

func (r *Roster) Reopen(id, reason string) (int, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return 0, ReopenReasonError{}
	}
	r.held.Lock()
	defer r.held.Unlock()
	for i := range r.agents {
		agent := &r.agents[i]
		if agent.ID != id {
			continue
		}
		if agent.Round >= konst.SubAgentMaxRounds {
			return 0, RoundCapError{Ticket: TicketID(agent.Brief), Cap: konst.SubAgentMaxRounds}
		}
		agent.Round++
		agent.State, agent.Report = Reopened, reason
		return agent.Round, nil
	}
	return 0, fmt.Errorf("subagent: %s is not on the roster, so it cannot be reopened", id)
}

func (r *Roster) Stepped(id string, steps int, at time.Time, calling ...string) {
	r.held.Lock()
	defer r.held.Unlock()
	for i := range r.agents {
		agent := &r.agents[i]
		if agent.ID != id {
			continue
		}
		agent.Steps, agent.Active = steps, at
		agent.Calling = append(agent.Calling, calling...)
		if made := len(agent.Calling) + agent.CallsDropped; made > konst.SubAgentCallsWatched {
			kept := konst.SubAgentCallsWatched - 1
			agent.Calling = slices.Delete(agent.Calling, 0, len(agent.Calling)-kept)
			agent.CallsDropped = made - kept
		}
		return
	}
}

func (r *Roster) Reached(id string, state State, report string) {
	r.held.Lock()
	defer r.held.Unlock()
	for i := range r.agents {
		if r.agents[i].ID == id {
			r.agents[i].State, r.agents[i].Report = state, report
			return
		}
	}
}

func (r *Roster) SubAgents() []SubAgent {
	r.held.Lock()
	defer r.held.Unlock()
	read := slices.Clone(r.agents)
	for i := range read {
		read[i].Calling = slices.Clone(read[i].Calling)
	}
	return read
}
