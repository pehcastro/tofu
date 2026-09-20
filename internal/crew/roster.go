package crew

import (
	"fmt"
	"strconv"
)

type State int

const (
	Working State = iota
	WaitingAnswer
	InReview
	Parked
	Errored
	Finished
)

func (s State) String() string {
	switch s {
	case Working:
		return "working"
	case WaitingAnswer:
		return "waiting_answer"
	case InReview:
		return "in_review"
	case Parked:
		return "parked"
	case Errored:
		return "errored"
	case Finished:
		return "finished"
	}
	panic("crew: unknown sub-agent state " + strconv.Itoa(int(s)))
}

type SubAgent struct {
	ID      string
	Mission string
	Brief   string
	Owns    []string
	State   State
	Report  string
}

type CollisionError struct {
	Child        string
	Glob         string
	Holder       string
	HolderGlob   string
	HolderReport string
}

func (e CollisionError) Error() string {
	return fmt.Sprintf("%s cannot hold %q: %s already holds %q and the two overlap", e.Child, e.Glob, e.Holder, e.HolderGlob)
}

type Roster struct {
	agents []SubAgent
}

func (r *Roster) Hold(agent SubAgent) error {
	for _, glob := range agent.Owns {
		if err := validGlob(glob); err != nil {
			return err
		}
		for _, held := range r.agents {
			for _, other := range held.Owns {
				if overlap(glob, other) {
					return CollisionError{Child: agent.ID, Glob: glob, Holder: held.ID, HolderGlob: other, HolderReport: held.Report}
				}
			}
		}
	}
	agent.State = Working
	r.agents = append(r.agents, agent)
	return nil
}

func (r *Roster) Reached(id string, state State, report string) {
	for i := range r.agents {
		if r.agents[i].ID == id {
			r.agents[i].State, r.agents[i].Report = state, report
			return
		}
	}
}

func (r *Roster) SubAgents() []SubAgent { return r.agents }
