package subagent

import (
	"cmp"
	"strconv"
	"time"

	"tofu/internal/konst"
	roster "tofu/internal/subagent"
)

func Children(agents []roster.SubAgent, now time.Time, steps int, spent map[string]int, calls func(roster.SubAgent) []Call) []Child {
	children := make([]Child, len(agents))
	for index, agent := range agents {
		since := agent.Active.Sub(agent.Started)
		if agent.State == roster.Working {
			since = now.Sub(agent.Started)
		}
		var watched []Call
		if calls != nil {
			watched = calls(agent)
		}
		children[index] = Child{
			Name:   "c" + strconv.Itoa(index+1),
			Owns:   agent.Owns,
			Doing:  agent.Mission,
			Since:  since,
			Steps:  agent.Steps,
			Total:  cmp.Or(steps, konst.TurnMaxSteps),
			Tokens: spent[agent.ID],
			State:  stateOf(agent.State),
			Calls:  watched,
			Report: agent.Report,
		}
	}
	return children
}

func stateOf(held roster.State) State {
	switch held {
	case roster.Working:
		return Running
	case roster.WaitingAnswer:
		return WaitingForAnswer
	case roster.InReview:
		return HandedBack
	case roster.Parked:
		return Parked
	case roster.Errored:
		return Errored
	case roster.Finished:
		return Done
	}
	panic("subagent: unknown roster state " + held.String())
}
