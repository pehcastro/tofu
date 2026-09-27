package subagent

import (
	"cmp"
	"time"

	"tofu/internal/konst"
	roster "tofu/internal/subagent"
)

type State = roster.State

type Call struct {
	ID     string
	At     time.Time
	Tool   string
	Text   string
	Result string
}

type Row struct {
	Name   string
	Agent  string
	Model  string
	Owns   []string
	Doing  string
	Since  time.Duration
	Steps  int
	Total  int
	Tokens int
	State  State
	Calls  []Call
	Report string
}

func Rows(agents []roster.SubAgent, now time.Time, steps int, spent map[string]int, calls func(roster.SubAgent) []Call) []Row {
	rows := make([]Row, len(agents))
	for index, agent := range agents {
		since := agent.Active.Sub(agent.Started)
		if agent.State == roster.Working {
			since = now.Sub(agent.Started)
		}
		var watched []Call
		if calls != nil {
			watched = calls(agent)
		}
		rows[index] = Row{
			Name:   agent.ID,
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
	return rows
}
