package host

import (
	"cmp"
	"encoding/json"
	"time"

	"tofu/internal/judge/jev"
	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/memory"
	"tofu/internal/session"
	roster "tofu/internal/subagent"
	"tofu/internal/turn"
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
	EventPersisted
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
	Identity  *session.Identity
	Promote   bool
	GateWhy   jev.Why
	Args      json.RawMessage
	ExitCode  *int
	Logged    *session.Event
	Status    Status
	Fork      *Fork
	Origin    Origin
	LastAt    time.Time
	Step      int
	Turn      string
	Questions []turn.PersonQuestion
	Wait      time.Duration
	Accepts   []ApprovalDecision
}

type Status string

const (
	StatusFinished Status = "finished"
	StatusStopped  Status = "stopped"
	StatusFailed   Status = "failed"
)

type Fork struct {
	From   string
	To     string
	Kind   string
	Before int
	After  int
}

func (e Event) snapshot() bool {
	switch e.Kind {
	case EventPersisted:
		return e.Logged == nil || e.Logged.Kind != session.EventMessage || e.Agent != ""
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
	Question string  `json:"question"`
	Choice   string  `json:"choice,omitempty"`
	Value    float64 `json:"value"`
	Max      float64 `json:"max"`
}

type Reason struct {
	Question  string   `json:"question"`
	Limit     string   `json:"limit"`
	Levels    []string `json:"levels"`
	Threshold float64  `json:"threshold"`
	Value     float64  `json:"value"`
	DeadBand  bool     `json:"deadBand"`
	RelaxedBy string   `json:"relaxedBy,omitempty"`
	Blocked   bool     `json:"blocked"`
}

type Decision struct {
	Tool          string
	Call          string
	Verdict       Verdict
	Answers       []GateAnswer
	Reason        Reason
	Failure       string
	Enforced      bool
	OverridesRule string
	Remembers     string
	MemoryScope   memory.Scope
	MemoryScopes  []memory.Scope
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
	Started time.Time
	Name    string
	Agent   string
	Model   string
	Owns    []string
	Doing   string
	Since   time.Duration
	Steps   int
	Total   int
	Tokens  int
	State   roster.State
	Calls   []Call
	Report  string
	Ended   time.Time
	Turn    string
	Parent  string
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
			Started: agent.Started,
			Name:    agent.ID,
			Agent:   agent.Agent,
			Model:   agent.Model,
			Owns:    agent.Owns,
			Doing:   agent.Mission,
			Since:   since,
			Steps:   agent.Steps,
			Total:   cmp.Or(steps, konst.TurnMaxSteps),
			Tokens:  spent[agent.ID],
			State:   agent.State,
			Calls:   watched,
			Report:  agent.Report,
		}
	}
	return rows
}

func (r *SubAgentRow) endedAs(run session.AgentRun) {
	if run.EndedAt != nil {
		r.Started, r.Ended, r.Turn = run.StartedAt, *run.EndedAt, run.SpawnTurn
	}
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
	NeverHere
)
