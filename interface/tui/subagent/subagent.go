package subagent

import (
	"time"

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

type Child struct {
	Name   string
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
