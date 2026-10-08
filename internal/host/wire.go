package host

import (
	"cmp"
	"encoding/json"
	"time"

	roster "tofu/internal/subagent"
)

const (
	Protocol       = "tofu.host/1"
	ApprovalMethod = "tofu/requestApproval"
	resyncMethod   = "resync"
	rpcVersion     = "2.0"
)

const (
	CodeParse       = -32700
	CodeInvalid     = -32600
	CodeNoMethod    = -32601
	CodeBadParams   = -32602
	CodeRefused     = -32000
	CodeSessionBusy = -32001
)

type Refusal struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (r *Refusal) Error() string { return r.Message }

type Identity struct {
	Session string `json:"session"`
	Turn    string `json:"turn"`
	Agent   string `json:"agent,omitempty"`
	Item    string `json:"item"`
	Seq     int64  `json:"seq"`
}

func (i *Identity) stamp(seq int64) { i.Seq = seq }

type enumerated interface{ enum() []string }

func (Status) enum() []string {
	return []string{string(StatusFinished), string(StatusStopped), string(StatusFailed)}
}

type EditOp string

const (
	EditCreate EditOp = "create"
	EditModify EditOp = "modify"
)

func (EditOp) enum() []string { return []string{string(EditCreate), string(EditModify)} }

type LineKind string

const (
	LineContext LineKind = "context"
	LineRemoved LineKind = "removed"
	LineAdded   LineKind = "added"
)

func (LineKind) enum() []string {
	return []string{string(LineContext), string(LineRemoved), string(LineAdded)}
}

type AgentState string

func (AgentState) enum() []string {
	var names []string
	for _, state := range roster.States() {
		names = append(names, state.String())
	}
	return names
}

type SaidKind string

const (
	SaidNote    SaidKind = "note"
	SaidGateOff SaidKind = "gate_off"
	SaidFailure SaidKind = "failure"
)

func (SaidKind) enum() []string {
	return []string{string(SaidNote), string(SaidGateOff), string(SaidFailure)}
}

type StepState string

const (
	StepPending StepState = "pending"
	StepRunning StepState = "running"
	StepDone    StepState = "done"
	StepDropped StepState = "dropped"
)

func (StepState) enum() []string {
	return []string{string(StepPending), string(StepRunning), string(StepDone), string(StepDropped)}
}

type VerdictName string

func (VerdictName) enum() []string { return []string{Allow.String(), Ask.String(), Deny.String()} }

type ApprovalDecision string

const (
	AllowOnce    ApprovalDecision = "allow_once"
	AllowAlways  ApprovalDecision = "allow_always"
	RejectOnce   ApprovalDecision = "reject_once"
	RejectAlways ApprovalDecision = "reject_always"
	Cancelled    ApprovalDecision = "cancelled"
)

func (ApprovalDecision) enum() []string {
	return []string{string(AllowOnce), string(AllowAlways), string(RejectOnce), string(RejectAlways), string(Cancelled)}
}

type AskingMode string

const (
	AskingAsk  AskingMode = "ask"
	AskingAuto AskingMode = "auto"
)

func (AskingMode) enum() []string { return []string{string(AskingAsk), string(AskingAuto)} }

type Marker struct {
	Identity
}

type Text struct {
	Identity
	Text string `json:"text"`
}

type TurnStarted struct {
	Identity
	Task      string    `json:"task"`
	StartedAt time.Time `json:"startedAt"`
}

type TurnCompleted struct {
	Identity
	Status      Status    `json:"status"`
	StartedAt   time.Time `json:"startedAt"`
	WorkedForMs int64     `json:"workedForMs"`
}

type ToolStarted struct {
	Identity
	Tool      string          `json:"tool"`
	Args      json.RawMessage `json:"args"`
	StartedAt time.Time       `json:"startedAt"`
}

type ToolCompleted struct {
	Identity
	Tool       string `json:"tool"`
	Failed     bool   `json:"failed"`
	ExitCode   *int   `json:"exitCode,omitempty"`
	Bytes      int    `json:"bytes"`
	Lines      int    `json:"lines"`
	DurationMs int64  `json:"durationMs"`
	Output     string `json:"output"`
}

type FileEdit struct {
	Identity
	Path  string `json:"path"`
	Op    EditOp `json:"op"`
	Hunks []Hunk `json:"hunks"`
}

type Hunk struct {
	OldStart int        `json:"oldStart"`
	OldLines int        `json:"oldLines"`
	NewStart int        `json:"newStart"`
	NewLines int        `json:"newLines"`
	Lines    []HunkLine `json:"lines"`
}

type HunkLine struct {
	Kind LineKind `json:"kind"`
	Text string   `json:"text"`
}

type AgentStarted struct {
	Identity
	Instance  string     `json:"instance"`
	Kind      string     `json:"kind"`
	Number    int        `json:"number"`
	Task      string     `json:"task"`
	Owns      []string   `json:"owns"`
	Model     string     `json:"model"`
	State     AgentState `json:"state"`
	StartedAt time.Time  `json:"startedAt"`
}

type AgentUpdated struct {
	Identity
	Instance string      `json:"instance"`
	State    *AgentState `json:"state,omitempty"`
	Task     *string     `json:"task,omitempty"`
	Model    *string     `json:"model,omitempty"`
	Steps    *int        `json:"steps,omitempty"`
	Tokens   *int        `json:"tokens,omitempty"`
	Report   *string     `json:"report,omitempty"`
}

func (a *AgentUpdated) absorb(older any) {
	was := older.(*AgentUpdated)
	a.State, a.Task, a.Model = cmp.Or(a.State, was.State), cmp.Or(a.Task, was.Task), cmp.Or(a.Model, was.Model)
	a.Steps, a.Tokens, a.Report = cmp.Or(a.Steps, was.Steps), cmp.Or(a.Tokens, was.Tokens), cmp.Or(a.Report, was.Report)
}

type AgentEnded struct {
	Identity
	Instance string     `json:"instance"`
	State    AgentState `json:"state"`
	Report   string     `json:"report"`
}

type ShellStarted struct {
	Identity
	Shell     string    `json:"shell"`
	Command   string    `json:"command"`
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"startedAt"`
}

type ShellOutput struct {
	Identity
	Shell  string `json:"shell"`
	Offset int    `json:"offset"`
	Text   string `json:"text"`
}

func (s *ShellOutput) absorb(older any) {
	was := older.(*ShellOutput)
	s.Offset, s.Text = was.Offset, was.Text+s.Text
}

type ShellExited struct {
	Identity
	Shell    string     `json:"shell"`
	ExitCode *int       `json:"exitCode,omitempty"`
	Killed   bool       `json:"killed"`
	EndedAt  *time.Time `json:"endedAt,omitempty"`
}

type Judgement struct {
	Verdict  VerdictName  `json:"verdict"`
	Answers  []GateAnswer `json:"answers"`
	Reason   *Reason      `json:"reason,omitempty"`
	Failure  string       `json:"failure,omitempty"`
	Enforced bool         `json:"enforced"`
}

type DecisionMade struct {
	Identity
	Judgement
	Point         string `json:"point"`
	Tool          string `json:"tool"`
	OverridesRule string `json:"overridesRule,omitempty"`
}

type Said struct {
	Identity
	Kind SaidKind `json:"kind"`
	Text string   `json:"text"`
}

type PlanUpdated struct {
	Identity
	Items []PlanStep `json:"items"`
}

type PlanStep struct {
	Phase string    `json:"phase"`
	Text  string    `json:"text"`
	State StepState `json:"state"`
}

type ContextUpdated struct {
	Identity
	Used   int `json:"used"`
	Budget int `json:"budget"`
}

type UsageUpdated struct {
	Identity
	Model     string `json:"model"`
	TokensIn  int    `json:"tokensIn"`
	TokensOut int    `json:"tokensOut"`
	CacheRead int    `json:"cacheRead"`
	Decisions int    `json:"decisions"`
}

type QuotaUpdated struct {
	Identity
	Windows []QuotaWindow `json:"windows"`
}

type QuotaWindow struct {
	Account  string     `json:"account"`
	Window   string     `json:"window"`
	Percent  float64    `json:"percent"`
	Reported bool       `json:"reported"`
	ResetsAt *time.Time `json:"resetsAt,omitempty"`
}

type CronState struct {
	Live  int       `json:"live"`
	Goals int       `json:"goals"`
	Jobs  []CronJob `json:"jobs"`
}

type CronJob struct {
	ID       string     `json:"id"`
	Schedule string     `json:"schedule"`
	Prompt   string     `json:"prompt"`
	Paused   bool       `json:"paused"`
	Next     *time.Time `json:"next,omitempty"`
	Ended    string     `json:"ended,omitempty"`
}

type CronUpdated struct {
	Identity
	CronState
}

type CronCommandParams struct {
	Line string `json:"line"`
}

type CronCommandResult struct {
	Note string `json:"note"`
}

type SessionForked struct {
	Identity
	From   string `json:"from"`
	To     string `json:"to"`
	Kind   string `json:"kind"`
	Before int    `json:"before"`
	After  int    `json:"after"`
}

type SessionUpdated struct {
	Identity
	Name       string    `json:"name"`
	Root       string    `json:"root"`
	Tag        string    `json:"tag,omitempty"`
	Generation int       `json:"generation,omitempty"`
	Handle     string    `json:"handle,omitempty"`
	Started    time.Time `json:"started,omitzero"`
}

type ApprovalRequest struct {
	Identity
	Approval string          `json:"approval"`
	Tool     string          `json:"tool"`
	Target   string          `json:"target"`
	Args     json.RawMessage `json:"args"`
	Judged   *Judgement      `json:"judged,omitempty"`
}

type ApprovalAnswer struct {
	Decision ApprovalDecision `json:"decision"`
}

type ApprovalResolved struct {
	Identity
	Approval string           `json:"approval"`
	Decision ApprovalDecision `json:"decision"`
	By       string           `json:"by"`
}

type Persisted struct {
	Identity
	LogSeq int    `json:"logSeq"`
	LogID  string `json:"logId"`
	Kind   string `json:"kind"`
}

type Resync struct {
	Identity
	Dropped int `json:"dropped"`
}

type InitializeParams struct {
	Client       string   `json:"client"`
	Versions     []string `json:"versions"`
	Capabilities []string `json:"capabilities"`
}

type InitializeResult struct {
	Protocol     string   `json:"protocol"`
	Tofu         string   `json:"tofu"`
	Project      string   `json:"project"`
	Capabilities []string `json:"capabilities"`
}

type SessionOpenParams struct {
	Project string     `json:"project,omitempty"`
	Session string     `json:"session,omitempty"`
	Asking  AskingMode `json:"asking,omitempty"`
}

type SessionRenameParams struct {
	Session string `json:"session"`
	Name    string `json:"name"`
}

type SessionOpenResult struct {
	Session string `json:"session"`
	Fresh   bool   `json:"fresh"`
}

type TurnSendParams struct {
	Session  string   `json:"session"`
	Text     string   `json:"text"`
	Mentions []string `json:"mentions,omitempty"`
	Model    string   `json:"model,omitempty"`
	Effort   string   `json:"effort,omitempty"`
}

type TurnSteerParams struct {
	Session        string `json:"session"`
	ExpectedTurnID string `json:"expectedTurnId"`
	Text           string `json:"text"`
}

type TurnParams struct {
	Session string `json:"session"`
	Turn    string `json:"turn"`
}

type TurnResult struct {
	Turn string `json:"turn"`
}

type UndoParams struct {
	Session string `json:"session"`
	Turns   int    `json:"turns"`
}

type ShellParams struct {
	Shell  string `json:"shell"`
	Offset int    `json:"offset,omitempty"`
}

type ShellReadResult struct {
	Shell  string `json:"shell"`
	Offset int    `json:"offset"`
	Text   string `json:"text"`
	Next   int    `json:"next"`
}

type LabelParams struct {
	Row     string `json:"row,omitempty"`
	Outcome string `json:"outcome"`
}

type SettingsSetParams struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Scope string `json:"scope,omitempty"`
}

type LoginParams struct {
	Role     string `json:"role"`
	Provider string `json:"provider"`
}

type NoParams struct{}

type Ack struct {
	OK bool `json:"ok"`
}

type VerbResult struct {
	Tofu     string    `json:"tofu"`
	Verb     string    `json:"verb"`
	OK       bool      `json:"ok"`
	At       string    `json:"at"`
	Data     any       `json:"data"`
	Problems []Problem `json:"problems"`
}

type Problem struct {
	What string `json:"what"`
	Hint string `json:"hint,omitempty"`
}

type method struct {
	name   string
	params any
	result any
}

func notifications() []method {
	return []method{
		{name: "turn.started", params: TurnStarted{}},
		{name: "turn.completed", params: TurnCompleted{}},
		{name: "turn.steered", params: Text{}},
		{name: "message.user", params: Text{}},
		{name: "message.started", params: Marker{}},
		{name: "message.delta", params: Text{}},
		{name: "message.completed", params: Text{}},
		{name: "message.reset", params: Marker{}},
		{name: "thinking.delta", params: Text{}},
		{name: "tool.started", params: ToolStarted{}},
		{name: "tool.completed", params: ToolCompleted{}},
		{name: "file.edit", params: FileEdit{}},
		{name: "agent.started", params: AgentStarted{}},
		{name: "agent.updated", params: AgentUpdated{}},
		{name: "agent.ended", params: AgentEnded{}},
		{name: "shell.started", params: ShellStarted{}},
		{name: "shell.output", params: ShellOutput{}},
		{name: "shell.exited", params: ShellExited{}},
		{name: "decision", params: DecisionMade{}},
		{name: "note", params: Said{}},
		{name: "failure", params: Said{}},
		{name: "plan.updated", params: PlanUpdated{}},
		{name: "context.updated", params: ContextUpdated{}},
		{name: "usage.updated", params: UsageUpdated{}},
		{name: "quota.updated", params: QuotaUpdated{}},
		{name: "cron.updated", params: CronUpdated{}},
		{name: "session.forked", params: SessionForked{}},
		{name: "session.updated", params: SessionUpdated{}},
		{name: "approval.resolved", params: ApprovalResolved{}},
		{name: "item.persisted", params: Persisted{}},
		{name: resyncMethod, params: Resync{}},
	}
}

func requests() []method {
	verb := VerbResult{}
	methods := []method{
		{name: "initialize", params: InitializeParams{}, result: InitializeResult{}},
		{name: "session.list", params: NoParams{}, result: verb},
		{name: "session.open", params: SessionOpenParams{}, result: SessionOpenResult{}},
		{name: "session.rename", params: SessionRenameParams{}, result: Ack{}},
		{name: "turn.send", params: TurnSendParams{}, result: TurnResult{}},
		{name: "turn.steer", params: TurnSteerParams{}, result: TurnResult{}},
		{name: "turn.stop", params: TurnParams{}, result: Ack{}},
		{name: "undo", params: UndoParams{}, result: verb},
		{name: "shell.read", params: ShellParams{}, result: ShellReadResult{}},
		{name: "shell.kill", params: ShellParams{}, result: Ack{}},
		{name: "label", params: LabelParams{}, result: verb},
		{name: "settings.set", params: SettingsSetParams{}, result: verb},
		{name: "login.start", params: LoginParams{}, result: verb},
		{name: "cron.command", params: CronCommandParams{}, result: CronCommandResult{}},
		{name: queryPrefix + "cron", params: NoParams{}, result: CronState{}},
	}
	for _, query := range queries() {
		methods = append(methods, method{name: queryPrefix + query.name, params: NoParams{}, result: verb})
	}
	return methods
}

const queryPrefix = "query."

type query struct {
	name string
	verb []string
}

func queries() []query {
	return []query{
		{name: "usage", verb: []string{"usage"}},
		{name: "context", verb: []string{"context"}},
		{name: "rules", verb: []string{"rules", "list"}},
		{name: "agents", verb: []string{"agents"}},
		{name: "models", verb: []string{"models"}},
		{name: "ledger", verb: []string{"why"}},
		{name: "settings", verb: []string{"settings"}},
		{name: "library", verb: []string{"library"}},
	}
}
