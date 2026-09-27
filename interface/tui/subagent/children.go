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
			Agent:  agent.Agent,
			Model:  agent.Model,
			Owns:   agent.Owns,
			Doing:  agent.Mission,
			Since:  since,
			Steps:  agent.Steps,
			Total:  cmp.Or(steps, konst.TurnMaxSteps),
			Tokens: spent[agent.ID],
			State:  agent.State,
			Calls:  watched,
			Report: agent.Report,
		}
	}
	return children
}
