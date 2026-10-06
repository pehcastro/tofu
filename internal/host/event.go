package host

import (
	"cmp"
	"time"

	"tofu/internal/judge/jev"
	"tofu/internal/konst"
	"tofu/internal/llm"
	roster "tofu/internal/subagent"
)

type EventKind int

const (
	EventText EventKind = iota
	EventTextDelta
	EventToolCall
	EventToolResult
	EventNote
	EventFailure
	EventStats
	EventDone
	EventDecision
	EventGateOff
	EventContext
	EventForkStart
	EventForkEnd
	EventSubAgent
	EventAwaitPerson
	EventResumed
	EventSteered
	EventRequesting
	EventPlan
	EventSession
	EventTask
	EventStreamReset
	EventThinking
	EventTurnStarted
	EventTurnEnded
)

type Event struct {
	Kind      EventKind
	ID        string
	Tool      string
	Text      string
	Detail    string
	Bytes     int
	Failed    bool
	Model     string
	TokensIn  int
	TokensOut int
	CacheRead int
	Decisions int
	Decision  *Decision
	Context   Context
	SubAgents []SubAgentRow
	Diff      string
	Plan      []PlanItem
	Created   string
	Agent     string
	Root      string
	Promote   bool
	GateWhy   jev.Why
}

func (e Event) snapshot() bool {
	switch e.Kind {
	case EventContext, EventSubAgent:
		return true
	case EventText, EventTextDelta, EventToolCall, EventToolResult, EventNote, EventFailure, EventStats, EventDone,
		EventDecision, EventGateOff, EventAwaitPerson, EventResumed, EventSteered, EventRequesting, EventPlan,
		EventSession, EventForkStart, EventForkEnd, EventTask, EventStreamReset, EventThinking, EventTurnStarted, EventTurnEnded:
		return false
	}
	panic("host: unknown event kind")
}

type Context struct {
	Used   int
	Budget int
}

type Verdict int

const (
	Allow Verdict = iota
	Ask
	Deny
)

func (v Verdict) String() string {
	switch v {
	case Allow:
		return "allow"
	case Ask:
		return "ask"
	case Deny:
		return "deny"
	}
	panic("host: unknown verdict")
}

type GateAnswer struct {
	Question string
	Choice   string
	Value    float64
	Max      float64
}

type Reason struct {
	Question  string
	Limit     string
	Levels    []string
	Threshold float64
	Value     float64
	DeadBand  bool
	RelaxedBy string
	Blocked   bool
}

type Decision struct {
	Tool          string
	Verdict       Verdict
	Answers       []GateAnswer
	Reason        Reason
	Failure       string
	Enforced      bool
	OverridesRule string
}

type PlanState int

const (
	PlanPending PlanState = iota
	PlanRunning
	PlanDone
	PlanDropped
)

type PlanItem struct {
	Phase string
	Text  string
	State PlanState
}

type Call struct {
	ID     string
	At     time.Time
	Tool   string
	Text   string
	Result string
}

type SubAgentRow struct {
	Name   string
	Agent  string
	Model  string
	Owns   []string
	Doing  string
	Since  time.Duration
	Steps  int
	Total  int
	Tokens int
	State  roster.State
	Calls  []Call
	Report string
}

func SubAgentRows(agents []roster.SubAgent, now time.Time, steps int, spent map[string]int, calls func(roster.SubAgent) []Call) []SubAgentRow {
	rows := make([]SubAgentRow, len(agents))
	for index, agent := range agents {
		since := agent.Active.Sub(agent.Started)
		if agent.State == roster.Working {
			since = now.Sub(agent.Started)
		}
		var watched []Call
		if calls != nil {
			watched = calls(agent)
		}
		rows[index] = SubAgentRow{
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

type Pick struct {
	Wire   string
	Model  string
	Effort llm.Effort
	Fired  string
}

type Answer int

const (
	Denied Answer = iota
	AllowedOnce
	AlwaysHere
)
