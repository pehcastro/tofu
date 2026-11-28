package host

import (
	"cmp"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/internal/agent/loop"
	roster "tofu/internal/agent/subagents"
	"tofu/internal/konst"
	"tofu/internal/models/classifiers/core"
	llm "tofu/internal/models/llm/core"
	"tofu/internal/store/memory/entries"
	"tofu/internal/store/session"
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
	EventMemoryScoped
	EventAccount
	EventSteerQueued
	EventMembers
	EventRequestSent
)

type Event struct {
	Kind         EventKind
	ID           string
	Tool         string
	Text         string
	Detail       string
	Bytes        int
	DurationMS   int64
	Untimed      bool        `json:",omitempty"`
	Outcome      ToolOutcome `json:",omitempty"`
	Model        string
	TokensIn     int
	TokensOut    int
	CacheRead    int
	CacheWrite5m int
	CacheWrite1h int
	CostUSD      float64
	TokenRate    float64
	Decisions    int
	Decision     *Decision
	Context      Context
	SubAgents    []SubAgentRow
	Diff         string
	Plan         []PlanItem
	Created      string
	Agent        string
	Root         string
	Identity     *session.Identity
	Promote      bool
	GateWhy      core.Why
	Args         json.RawMessage
	ExitCode     *int
	Hits         *int
	Logged       *session.Event
	Status       Status
	Fork         *Fork
	Origin       Origin
	LastAt       time.Time
	At           time.Time
	Step         int
	Turn         string
	Questions    []loop.PersonQuestion
	Resolution   *Resolution `json:",omitempty"`
	Wait         time.Duration
	Accepts      []ApprovalDecision
	Reason       string
	Risk         Risk
	Scoped       *MemoryScoped
	Account      *TurnAccount
	Images       []MessageImage
	Members      []Member
	Card         *ToolCard
	Group        string
	GroupSummary *GroupSummary
	Judge        *JudgeStatus `json:",omitempty"`
	Limit        *LimitWait   `json:",omitempty"`
	from         *Host
}

type Status string

const (
	StatusFinished Status = "finished"
	StatusStopped  Status = "stopped"
	StatusFailed   Status = "failed"
)

type ToolOutcome string

const (
	ToolRan     ToolOutcome = session.ToolOutcomeRan
	ToolFailed  ToolOutcome = session.ToolOutcomeFailed
	ToolAborted ToolOutcome = session.ToolOutcomeAborted
	ToolSkipped ToolOutcome = session.ToolOutcomeSkipped
)

func (ToolOutcome) enum() []string {
	return names([]ToolOutcome{ToolRan, ToolFailed, ToolAborted, ToolSkipped})
}

func (o ToolOutcome) Failed() bool { return o == ToolFailed || o == ToolSkipped }

type unknownOutcome string

func (u unknownOutcome) Error() string {
	return "host: a tool result names the outcome " + string(u) + ", which this build does not know"
}

func (o *ToolOutcome) UnmarshalJSON(raw []byte) error {
	var said string
	if json.Unmarshal(raw, &said) != nil || !slices.Contains(o.enum(), said) {
		return unknownOutcome(raw)
	}
	*o = ToolOutcome(said)
	return nil
}

func outcomeOf(ran llm.ToolOutcome) ToolOutcome {
	switch ran {
	case llm.ToolOutcomeUnset, llm.ToolOutcomeRan:
		return ToolRan
	case llm.ToolOutcomeFailed:
		return ToolFailed
	case llm.ToolOutcomeAborted:
		return ToolAborted
	case llm.ToolOutcomeSkipped:
		return ToolSkipped
	}
	panic("host: unknown tool outcome " + strconv.Itoa(int(ran)))
}

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
	case EventContext, EventSubAgent, EventMembers:
		return true
	case EventText, EventTextDelta, EventToolCall, EventToolResult, EventNote, EventFailure, EventStats, EventDone,
		EventDecision, EventGateOff, EventAwaitPerson, EventResumed, EventSteerQueued, EventSteered, EventRequesting, EventPlan,
		EventSession, EventForkStart, EventForkEnd, EventTask, EventStreamReset, EventThinking, EventTurnStarted, EventTurnEnded, EventMemoryScoped, EventAccount, EventRequestSent:
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
	Provider      string
	Model         string
	DurationMS    int64
	Enforced      bool
	OverridesRule string
	Remembers     string
	MemoryScope   entries.Scope `json:",omitempty"`
	MemoryScopes  []entries.Scope
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
	Check string `json:",omitempty"`
	Call  string `json:",omitempty"`
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
	Ticket  string
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
	Moves   []roster.Transition
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
			Ticket:  agent.Ticket,
			Doing:   agent.Mission,
			Since:   since,
			Steps:   agent.Steps,
			Total:   cmp.Or(steps, konst.TurnMaxSteps),
			Tokens:  spent[agent.ID],
			State:   agent.State,
			Calls:   watched,
			Report:  agent.Report,
			Moves:   agent.Moves,
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
}

type Resolution struct {
	Decision ApprovalDecision
	By       string
	Standing bool
	Until    time.Time
}

func cancelledByTofu() *Resolution { return &Resolution{Decision: Cancelled, By: "tofu"} }

type Answer int

const (
	Denied Answer = iota
	AllowedOnce
	AlwaysHere
	NeverHere
	AllowedAWhile
)

type LimitState string

const (
	LimitWaiting  LimitState = "waiting"
	LimitRefused  LimitState = "refused"
	LimitResuming LimitState = "resuming"
	LimitGaveUp   LimitState = "gave_up"
	LimitCleared  LimitState = "cleared"
)

type LimitWait struct {
	State   LimitState
	Source  string
	ResetAt time.Time
	TryAt   time.Time
	Tries   int
}

type LimitPart struct {
	Text  string
	Value bool
}

func (w LimitWait) Parts(now time.Time) []LimitPart {
	name, subscription := strings.CutSuffix(w.Source, "-sub")
	named := strings.ToUpper(name[:1]) + name[1:]
	account, vendor := named+" account", named
	if subscription {
		account = named + " subscription"
	}
	switch name {
	case "claude":
		vendor = "Anthropic"
	case "codex":
		vendor = "OpenAI"
	}
	said := func(text string) LimitPart { return LimitPart{Text: text} }
	value := func(text string) LimitPart { return LimitPart{Text: text, Value: true} }
	switch w.State {
	case LimitWaiting:
		return []LimitPart{said(account + " is out of usage until "), value(w.ResetAt.Local().Format("Mon 15:04")), said(". Auto resume in "), value(countdown(w.TryAt.Sub(now)))}
	case LimitResuming:
		return []LimitPart{said("Resuming on your " + account + "...")}
	case LimitRefused:
		return []LimitPart{said("Your reset was scheduled for "), value(w.ResetAt.Local().Format("15:04")), said(", " + vendor + " has still not released it, trying again in "),
			value(countdown(w.TryAt.Sub(now))), said(", this is the "), value(ordinal(w.Tries)), said(" try.")}
	case LimitGaveUp:
		return []LimitPart{said("Your " + account + " did not reset in the scheduled time, try again later or check your " + vendor + " usage.")}
	case LimitCleared:
		return nil
	}
	panic("host: unknown limit state " + string(w.State))
}

func (w *LimitWait) note(now time.Time) Event {
	var said strings.Builder
	for _, part := range w.Parts(now) {
		said.WriteString(part.Text)
	}
	return Event{Kind: EventNote, Text: said.String(), Limit: w}
}

func countdown(left time.Duration) string {
	left = max(left, 0)
	hours, minutes, seconds := int(left/time.Hour), int(left%time.Hour/time.Minute), int(left%time.Minute/time.Second)
	switch {
	case hours > 0:
		return strconv.Itoa(hours) + "h " + strconv.Itoa(minutes) + "m"
	case minutes >= konst.LimitSecondsUnderMinutes:
		return strconv.Itoa(minutes) + "m"
	case minutes > 0:
		return strconv.Itoa(minutes) + "m " + strconv.Itoa(seconds) + "s"
	}
	return strconv.Itoa(seconds) + "s"
}

func ordinal(n int) string {
	suffix := "th"
	switch {
	case n%100 >= 11 && n%100 <= 13:
	case n%10 == 1:
		suffix = "st"
	case n%10 == 2:
		suffix = "nd"
	case n%10 == 3:
		suffix = "rd"
	}
	return strconv.Itoa(n) + suffix
}
