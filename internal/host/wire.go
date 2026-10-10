package host

import (
	"cmp"
	"encoding/json"
	"time"

	"tofu/internal/boardy"
	"tofu/internal/command"
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/state"
	"tofu/internal/learn"
	"tofu/internal/llm"
	"tofu/internal/memory"
	"tofu/internal/shell"
	"tofu/internal/snapshot"
	roster "tofu/internal/subagent"
	"tofu/internal/sys"
	"tofu/internal/turn"
	"tofu/internal/turn/tools"
)

const (
	Protocol       = "tofu.host/1"
	ApprovalMethod = "tofu/requestApproval"
	QuestionMethod = "tofu/askPerson"
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
	Ref     string `json:"ref,omitempty"`
	Seq     int64  `json:"seq"`
}

func (i *Identity) stamp(seq int64) { i.Seq = seq }

func (i *Identity) refer() {
	i.Ref = tools.QuoteRef(i.Item)
	if i.Item != "" && i.Item == i.Agent {
		i.Ref = "[&" + i.Agent + "]"
	}
}

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
	AllowOnce            ApprovalDecision = "allow_once"
	AllowAlways          ApprovalDecision = "allow_always"
	RejectOnce           ApprovalDecision = "reject_once"
	RejectAlways         ApprovalDecision = "reject_always"
	Cancelled            ApprovalDecision = "cancelled"
	RememberProject      ApprovalDecision = "remember_project"
	RememberGlobal       ApprovalDecision = "remember_global"
	RememberUserLocal    ApprovalDecision = "remember_user_local"
	RememberProjectLocal ApprovalDecision = "remember_project_local"
)

func (ApprovalDecision) enum() []string {
	return []string{string(AllowOnce), string(AllowAlways), string(RejectOnce), string(RejectAlways), string(Cancelled), string(RememberProject), string(RememberGlobal), string(RememberUserLocal), string(RememberProjectLocal)}
}

func keptAs(decision ApprovalDecision) memory.Scope {
	switch decision {
	case RememberGlobal:
		return memory.Global
	case RememberProject:
		return memory.Project
	case RememberUserLocal:
		return memory.UserLocal
	case RememberProjectLocal:
		return memory.ProjectLocal
	}
	return ""
}

func rememberedAs(scope memory.Scope) ApprovalDecision {
	switch scope {
	case memory.Global:
		return RememberGlobal
	case memory.Project:
		return RememberProject
	case memory.UserLocal:
		return RememberUserLocal
	case memory.ProjectLocal:
		return RememberProjectLocal
	}
	panic("host: unknown memory scope " + string(scope))
}

func personAnswerOf(decision ApprovalDecision) Answer {
	switch decision {
	case AllowOnce:
		return AllowedOnce
	case AllowAlways:
		return AlwaysHere
	case RejectOnce:
		return Denied
	case RejectAlways:
		return NeverHere
	}
	panic("host: no answer for the decision " + string(decision))
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

type Steered struct {
	Identity
	Text   string    `json:"text"`
	Step   int       `json:"step"`
	ReadAt time.Time `json:"readAt"`
}

type OriginKind string

const (
	OriginPerson OriginKind = "person"
	OriginCron   OriginKind = "cron"
	OriginAgent  OriginKind = "agent"
	OriginTofu   OriginKind = "tofu"
)

func (OriginKind) enum() []string {
	return []string{string(OriginPerson), string(OriginCron), string(OriginAgent), string(OriginTofu)}
}

type Origin struct {
	Kind     OriginKind `json:"kind"`
	Job      string     `json:"job,omitempty"`
	Schedule string     `json:"schedule,omitempty"`
	Name     string     `json:"name,omitempty"`
	Source   string     `json:"source,omitempty"`
}

type UserMessage struct {
	Identity
	Text   string `json:"text"`
	Origin Origin `json:"origin"`
}

type TurnStarted struct {
	Identity
	Task      string    `json:"task"`
	Origin    Origin    `json:"origin"`
	StartedAt time.Time `json:"startedAt"`
}

type TurnAccount struct {
	Identity
	Source      string        `json:"source"`
	AccountID   int64         `json:"account_id"`
	Login       string        `json:"login,omitempty"`
	Model       string        `json:"model"`
	Reason      AccountReason `json:"reason"`
	FromAccount int64         `json:"from_account,omitempty"`
}

type AccountReason string

func (AccountReason) enum() []string {
	return []string{string(turn.AccountPicked), string(turn.AccountMoved)}
}

type TurnCompleted struct {
	Identity
	Status      Status    `json:"status"`
	StartedAt   time.Time `json:"startedAt"`
	WorkedForMs int64     `json:"workedForMs"`
}

type ToolStarted struct {
	Identity
	Instance  string          `json:"instance,omitempty"`
	Tool      string          `json:"tool"`
	Args      json.RawMessage `json:"args"`
	StartedAt time.Time       `json:"startedAt"`
}

type ToolCompleted struct {
	Identity
	Instance   string `json:"instance,omitempty"`
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
	Instance string `json:"instance,omitempty"`
	Path     string `json:"path"`
	Op       EditOp `json:"op"`
	Hunks    []Hunk `json:"hunks"`
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
	Ticket    string     `json:"ticket,omitempty"`
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
	Instance   string     `json:"instance"`
	State      AgentState `json:"state"`
	Report     string     `json:"report"`
	EndedAt    time.Time  `json:"endedAt"`
	DurationMs int64      `json:"durationMs"`
}

type ShellStarted struct {
	Identity
	Shell     string     `json:"shell"`
	Command   string     `json:"command"`
	PID       int        `json:"pid"`
	StartedAt time.Time  `json:"startedAt"`
	Kept      ShellKept  `json:"kept,omitempty"`
	Dir       string     `json:"dir"`
	Port      int        `json:"port,omitempty"`
	Ready     ShellReady `json:"ready,omitempty"`
	LeftOver  bool       `json:"leftOver,omitempty"`
	call      string
}

type ShellListening struct {
	Identity
	Shell string `json:"shell"`
	Port  int    `json:"port"`
}

func (s *ShellStarted) refer() {
	s.Identity.refer()
	s.Ref = cmp.Or(tools.QuoteRef(s.call), s.Ref)
}

type ShellKept string

func (ShellKept) enum() []string {
	return []string{string(shell.KeptBackground), string(shell.KeptMoved)}
}

type ShellReady string

func (ShellReady) enum() []string {
	return []string{string(shell.ReadyPort), string(shell.ReadyLine), string(shell.ReadyWaited), string(shell.ReadyStopped)}
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
	Point         string    `json:"point"`
	Tool          string    `json:"tool"`
	OverridesRule string    `json:"overridesRule,omitempty"`
	Call          string    `json:"call,omitempty"`
	At            time.Time `json:"at"`
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
	ContextUse
}

type ContextUse struct {
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
	Account   string     `json:"account"`
	Window    string     `json:"window"`
	Percent   float64    `json:"percent"`
	Reported  bool       `json:"reported"`
	ResetsAt  *time.Time `json:"resetsAt,omitempty"`
	Source    string     `json:"source,omitempty"`
	AccountID int64      `json:"account_id,omitempty"`
	Stale     bool       `json:"stale"`
	ReadAt    time.Time  `json:"read_at,omitzero"`
}

type AccountCondition string

const (
	ConditionServing     AccountCondition = "serving"
	ConditionRateLimited AccountCondition = "rate_limited"
	ConditionSpent       AccountCondition = "spent"
)

func (AccountCondition) enum() []string {
	return []string{string(ConditionServing), string(ConditionRateLimited), string(ConditionSpent)}
}

type AccountNow struct {
	Source    string           `json:"source"`
	AccountID int64            `json:"account_id"`
	State     AccountCondition `json:"state"`
	RetryAt   time.Time        `json:"retry_at,omitzero"`
}

type AccountStateChanged struct {
	Identity
	AccountNow
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
	LastAt     time.Time `json:"lastAt,omitzero"`
}

type ApprovalRequest struct {
	Identity
	Approval  string             `json:"approval"`
	Tool      string             `json:"tool"`
	Target    string             `json:"target"`
	Args      json.RawMessage    `json:"args"`
	Judged    *Judgement         `json:"judged,omitempty"`
	Decisions []ApprovalDecision `json:"decisions"`
}

type StandingAnswer struct {
	Target   string           `json:"target"`
	Decision ApprovalDecision `json:"decision"`
}

type ApprovalAnswer struct {
	Decision ApprovalDecision `json:"decision"`
}

type QuestionOutcome string

const (
	QuestionSubmitted   QuestionOutcome = "submitted"
	QuestionCancelled   QuestionOutcome = "cancelled"
	QuestionUndelivered QuestionOutcome = "undelivered"
)

func (QuestionOutcome) enum() []string {
	return []string{string(QuestionSubmitted), string(QuestionCancelled), string(QuestionUndelivered)}
}

type QuestionRequest struct {
	Identity
	Question  string                `json:"question"`
	Questions []turn.PersonQuestion `json:"questions"`
	Blocking  bool                  `json:"blocking"`
	WaitMs    int64                 `json:"waitMs"`
}

type QuestionAnswer struct {
	Outcome QuestionOutcome    `json:"outcome"`
	Answers []turn.PersonReply `json:"answers"`
}

type QuestionResolved struct {
	Identity
	Question string          `json:"question"`
	Outcome  QuestionOutcome `json:"outcome"`
	By       string          `json:"by"`
}

type ApprovalResolved struct {
	Identity
	Approval string           `json:"approval"`
	Decision ApprovalDecision `json:"decision"`
	By       string           `json:"by"`
	Standing bool             `json:"standing,omitempty"`
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
	Replay  *int       `json:"replay,omitempty"`
}

type SessionHistoryParams struct {
	Session string `json:"session"`
	Before  *int   `json:"before,omitempty"`
	Limit   int    `json:"limit,omitempty"`
}

type SessionHistory struct {
	Session string        `json:"session"`
	First   int           `json:"first"`
	Total   int           `json:"total"`
	Lines   []HistoryLine `json:"lines"`
}

type HistoryLine struct {
	Method string `json:"method"`
	Params any    `json:"params"`
}

type Compaction struct {
	Into         string `json:"into,omitempty"`
	Results      int    `json:"results"`
	TokensBefore int    `json:"tokensBefore"`
	TokensAfter  int    `json:"tokensAfter"`
}

type SessionRenameParams struct {
	Session string `json:"session"`
	Name    string `json:"name"`
}

type SessionCloseParams struct {
	Session string `json:"session"`
	Stop    bool   `json:"stop,omitempty"`
}

type SessionOpenResult struct {
	Session string `json:"session"`
	Fresh   bool   `json:"fresh"`
}

type SessionListParams struct {
	Search string `json:"search,omitempty"`
	Limit  int    `json:"limit,omitempty"`
	Kind   string `json:"kind,omitempty"`
}

const (
	KindMain = "main"
	KindSide = "side"
)

type SessionBranchParams struct {
	Session string           `json:"session"`
	Kind    string           `json:"kind"`
	Seed    string           `json:"seed,omitempty"`
	Owns    []string         `json:"owns,omitempty"`
	Preset  string           `json:"preset,omitempty"`
	Name    string           `json:"name,omitempty"`
	Model   string           `json:"model,omitempty"`
	Effort  llm.Effort       `json:"effort,omitempty"`
	At      *SessionBranchAt `json:"at,omitempty"`
}

type SessionBranchAt struct {
	Item string `json:"item"`
}

type SessionParent struct {
	Session string `json:"session"`
	Event   string `json:"event,omitempty"`
}

type SessionBranchResult struct {
	Session string        `json:"session"`
	Handle  string        `json:"handle"`
	Parent  SessionParent `json:"parent"`
	Owns    []string      `json:"owns"`
	Preset  string        `json:"preset,omitempty"`
	Carried int           `json:"carried"`
	Model   string        `json:"model,omitempty"`
	Effort  llm.Effort    `json:"effort,omitempty"`
}

type SessionAccessParams struct {
	Session string   `json:"session"`
	Owns    []string `json:"owns,omitempty"`
	Preset  string   `json:"preset,omitempty"`
}

type SessionAccess struct {
	Session string   `json:"session"`
	Owns    []string `json:"owns"`
	Preset  string   `json:"preset,omitempty"`
}

type SessionList struct {
	Head     string       `json:"head,omitempty"`
	Sessions []SessionRow `json:"sessions"`
}

type SessionRow struct {
	ID          string         `json:"id"`
	Name        string         `json:"name,omitempty"`
	Handle      string         `json:"handle"`
	Family      string         `json:"family,omitempty"`
	Generation  int            `json:"generation,omitempty"`
	Generations int            `json:"generations,omitempty"`
	At          time.Time      `json:"at"`
	LastAt      time.Time      `json:"lastAt,omitzero"`
	Task        string         `json:"task,omitempty"`
	Turns       int            `json:"turns"`
	SubAgents   int            `json:"subAgents"`
	Wire        string         `json:"wire,omitempty"`
	Model       string         `json:"model,omitempty"`
	CostUSD     float64        `json:"costUsd,omitempty"`
	EndedAt     *time.Time     `json:"endedAt,omitempty"`
	EndReason   string         `json:"endReason,omitempty"`
	Outcome     string         `json:"outcome,omitempty"`
	Expired     bool           `json:"expired"`
	Open        bool           `json:"open"`
	Running     bool           `json:"running"`
	HeldBy      *SessionHolder `json:"heldBy,omitempty"`
	Kind        string         `json:"kind"`
	Parent      *SessionParent `json:"parent,omitempty"`
	Owns        []string       `json:"owns,omitempty"`
	Preset      string         `json:"preset,omitempty"`
}

type SessionHolder struct {
	PID   int       `json:"pid"`
	Since time.Time `json:"since"`
}

type SessionListed struct {
	Identity
	SessionRow
}

type ModelPick struct {
	Wire   string     `json:"wire,omitempty"`
	Model  string     `json:"model,omitempty"`
	Effort llm.Effort `json:"effort,omitempty"`
}

type SessionSetParams struct {
	Asking AskingMode `json:"asking,omitempty"`
	ModelPick
}

type SessionSettings struct {
	Identity
	Asking AskingMode `json:"asking,omitempty"`
	Pick   ModelPick  `json:"pick"`
}

type SessionState struct {
	Session   string            `json:"session"`
	Running   bool              `json:"running"`
	Turn      *RunningTurn      `json:"turn,omitempty"`
	Asking    AskingMode        `json:"asking,omitempty"`
	Pick      ModelPick         `json:"pick"`
	Approvals []ApprovalRequest `json:"approvals"`
	Standing  []StandingAnswer  `json:"standing"`
	Questions []QuestionRequest `json:"questions"`
	Agents    []AgentNow        `json:"agents"`
	Shells    []ShellNow        `json:"shells"`
	Context   *ContextUse       `json:"context,omitempty"`
	Cron      CronState         `json:"cron"`
}

type RunningTurn struct {
	ID        string    `json:"id"`
	Task      string    `json:"task"`
	StartedAt time.Time `json:"startedAt"`
}

type AgentNow struct {
	Instance  string     `json:"instance"`
	Kind      string     `json:"kind"`
	Task      string     `json:"task"`
	Owns      []string   `json:"owns"`
	Model     string     `json:"model"`
	State     AgentState `json:"state"`
	Steps     int        `json:"steps"`
	Tokens    int        `json:"tokens"`
	StartedAt time.Time  `json:"startedAt"`
}

type ShellNow struct {
	Shell     string     `json:"shell"`
	Command   string     `json:"command"`
	PID       int        `json:"pid"`
	State     ShellState `json:"state"`
	StartedAt time.Time  `json:"startedAt"`
	ExitCode  *int       `json:"exitCode,omitempty"`
	EndedAt   *time.Time `json:"endedAt,omitempty"`
	Kept      ShellKept  `json:"kept,omitempty"`
	Dir       string     `json:"dir"`
	Port      int        `json:"port,omitempty"`
	Ready     ShellReady `json:"ready,omitempty"`
	LeftOver  bool       `json:"leftOver,omitempty"`
	Ref       string     `json:"ref,omitempty"`
}

type ShellState string

func (ShellState) enum() []string {
	return []string{string(shell.Running), string(shell.Exited), string(shell.Killed)}
}

type TurnSendParams struct {
	Session  string          `json:"session"`
	Text     string          `json:"text"`
	Mentions []string        `json:"mentions,omitempty"`
	Images   []AttachedImage `json:"images,omitempty"`
	ModelPick
}

type AttachedImage struct {
	Path string `json:"path"`
}

type TurnSteerParams struct {
	Session        string `json:"session"`
	ExpectedTurnID string `json:"expectedTurnId"`
	Text           string `json:"text"`
}

type TurnParams struct {
	Session string `json:"session"`
	Turn    string `json:"turn"`
	Lead    bool   `json:"lead,omitempty"`
}

type SteerResult struct {
	Turn string `json:"turn"`
	ID   string `json:"id"`
}

type SendNowParams struct {
	ID string `json:"id,omitempty"`
}

type UnsteerParams struct {
	Text string `json:"text"`
}

type UnsteerResult struct {
	Removed bool `json:"removed"`
}

type ShellRunParams struct {
	Command string `json:"command"`
}

type ShellRunResult struct {
	Output  string `json:"output"`
	Stopped bool   `json:"stopped"`
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

type LedgerParams struct {
	ID      string `json:"id,omitempty"`
	Last    int    `json:"last,omitempty"`
	Point   string `json:"point,omitempty"`
	Session string `json:"session,omitempty"`
}

type LedgerReport struct {
	Call *LedgerCall `json:"call,omitempty"`
	Rows []LedgerRow `json:"rows"`
}

type LedgerCall struct {
	Session string           `json:"session"`
	Step    int              `json:"step"`
	Attempt int              `json:"attempt"`
	Call    turn.ToolCallRow `json:"call"`
}

type LedgerRow struct {
	ledger.Row
	Chain      *ledger.Row       `json:"chain,omitempty"`
	BlockedBy  string            `json:"blocked_by,omitempty"`
	Precedents []LedgerPrecedent `json:"precedents"`
	Subject    *state.Subject    `json:"subject,omitempty"`
}

type LedgerPrecedent struct {
	ID              string          `json:"row_id"`
	Verdict         ledger.Verdict  `json:"verdict"`
	At              time.Time       `json:"at"`
	Distance        float64         `json:"distance"`
	SameFingerprint bool            `json:"same_fingerprint"`
	Comparable      bool            `json:"comparable"`
	Why             string          `json:"why"`
	Outcome         *ledger.Outcome `json:"outcome,omitempty"`
}

type LabelParams struct {
	Row     string `json:"row,omitempty"`
	Outcome string `json:"outcome"`
}

type LabelResult struct {
	ID      string         `json:"id"`
	Outcome ledger.Verdict `json:"outcome"`
	Kind    string         `json:"kind"`
	Verdict ledger.Verdict `json:"verdict"`
}

type SettingScope string

const (
	SettingGlobal  SettingScope = "global"
	SettingProject SettingScope = "project"
)

func (SettingScope) enum() []string { return []string{string(SettingGlobal), string(SettingProject)} }

type SettingsSetParams struct {
	Key   string       `json:"key"`
	Value string       `json:"value"`
	Scope SettingScope `json:"scope,omitempty"`
}

type SettingsSet struct {
	Key    string       `json:"key"`
	Value  any          `json:"value"`
	Scope  SettingScope `json:"scope"`
	Source string       `json:"source"`
}

type LoginParams struct {
	Role     string `json:"role"`
	Provider string `json:"provider"`
}

type LoginStarted struct {
	ID      int64  `json:"id"`
	Source  string `json:"source"`
	Account string `json:"account"`
	Change  string `json:"change"`
}

type NoParams struct{}

type Ack struct {
	OK bool `json:"ok"`
}

type VerbResult struct {
	Tofu     string          `json:"tofu"`
	Verb     string          `json:"verb"`
	OK       bool            `json:"ok"`
	At       string          `json:"at"`
	Data     json.RawMessage `json:"data"`
	Problems []Problem       `json:"problems"`
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
		{name: "turn.account", params: TurnAccount{}},
		{name: "turn.completed", params: TurnCompleted{}},
		{name: "turn.steered", params: Steered{}},
		{name: "message.user", params: UserMessage{}},
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
		{name: "shell.ready", params: ShellListening{}},
		{name: "shell.output", params: ShellOutput{}},
		{name: "shell.exited", params: ShellExited{}},
		{name: "decision", params: DecisionMade{}},
		{name: "note", params: Said{}},
		{name: "failure", params: Said{}},
		{name: "plan.updated", params: PlanUpdated{}},
		{name: "context.updated", params: ContextUpdated{}},
		{name: "usage.updated", params: UsageUpdated{}},
		{name: "quota.updated", params: QuotaUpdated{}},
		{name: "account.state", params: AccountStateChanged{}},
		{name: "cron.updated", params: CronUpdated{}},
		{name: "session.forked", params: SessionForked{}},
		{name: "session.updated", params: SessionUpdated{}},
		{name: "session.listed", params: SessionListed{}},
		{name: "session.turns.updated", params: SessionTurnUpdated{}},
		{name: "session.settings", params: SessionSettings{}},
		{name: "settings.changed", params: SettingsChanged{}},
		{name: "approval.resolved", params: ApprovalResolved{}},
		{name: "question.resolved", params: QuestionResolved{}},
		{name: "memory.scoped", params: MemoryScopedEvent{}},
		{name: "item.persisted", params: Persisted{}},
		{name: statusMethod, params: StatusReport{}},
		{name: resyncMethod, params: Resync{}},
		{name: boardyPrefix + "changed", params: boardy.Changed{}},
		{name: scratchPrefix + "changed", params: sys.ScratchReport{}},
	}
}

func requests() []method {
	methods := []method{
		{name: "initialize", params: InitializeParams{}, result: InitializeResult{}},
		{name: "session.list", params: SessionListParams{}, result: SessionList{}},
		{name: "session.open", params: SessionOpenParams{}, result: SessionOpenResult{}},
		{name: "session.close", params: SessionCloseParams{}, result: Ack{}},
		{name: "session.state", params: NoParams{}, result: SessionState{}},
		{name: "session.set", params: SessionSetParams{}, result: Ack{}},
		{name: "session.rename", params: SessionRenameParams{}, result: Ack{}},
		{name: "session.branch", params: SessionBranchParams{}, result: SessionBranchResult{}},
		{name: "session.access", params: SessionAccessParams{}, result: SessionAccess{}},
		{name: "turn.send", params: TurnSendParams{}, result: TurnResult{}},
		{name: "turn.steer", params: TurnSteerParams{}, result: SteerResult{}},
		{name: "turn.sendNow", params: SendNowParams{}, result: Ack{}},
		{name: "turn.stop", params: TurnParams{}, result: Ack{}},
		{name: "turn.unsteer", params: UnsteerParams{}, result: UnsteerResult{}},
		{name: "session.compact", params: SessionParams{}, result: Compaction{}},
		{name: "session.history", params: SessionHistoryParams{}, result: SessionHistory{}},
		{name: "session.turns", params: SessionParams{}, result: SessionTurns{}},
		{name: "shell.run", params: ShellRunParams{}, result: ShellRunResult{}},
		{name: "undo", params: UndoParams{}, result: snapshot.Report{}},
		{name: "shell.read", params: ShellParams{}, result: ShellReadResult{}},
		{name: "shell.kill", params: ShellParams{}, result: Ack{}},
		{name: "label", params: LabelParams{}, result: LabelResult{}},
		{name: "settings.set", params: SettingsSetParams{}, result: SettingsSet{}},
		{name: "login.start", params: LoginParams{}, result: LoginStarted{}},
		{name: "cron.command", params: CronCommandParams{}, result: CronCommandResult{}},
		{name: queryPrefix + "cron", params: NoParams{}, result: CronState{}},
		{name: queryPrefix + "ledger", params: LedgerParams{}, result: LedgerReport{}},
		{name: queryPrefix + "models", params: NoParams{}, result: ModelsQuery{}},
		{name: queryPrefix + "settings", params: NoParams{}, result: SettingsReport{}},
		{name: queryPrefix + "rules", params: NoParams{}, result: RuleListReport{}},
		{name: queryPrefix + "agents", params: NoParams{}, result: roster.Found{}},
		{name: queryPrefix + "library", params: NoParams{}, result: LibraryReport{}},
		{name: queryPrefix + "memory", params: NoParams{}, result: MemoryReport{}},
		{name: queryPrefix + "hooks", params: NoParams{}, result: HooksReport{}},
		{name: queryPrefix + "changelog", params: NoParams{}, result: ChangelogReport{}},
		{name: queryPrefix + "update", params: NoParams{}, result: UpdateReport{}},
		{name: queryPrefix + "docs", params: DocsParams{}, result: DocsAnswer{}},
		{name: "memory.view", params: MemoryViewParams{}, result: MemoryView{}},
		{name: "memory.zoom", params: MemoryZoomParams{}, result: MemoryView{}},
		{name: "memory.recall", params: MemoryRecallParams{}, result: MemoryView{}},
		{name: "memory.add", params: MemoryAddParams{}, result: memory.Entry{}},
		{name: "memory.edit", params: MemoryEditParams{}, result: memory.Entry{}},
		{name: "memory.remove", params: MemoryRemoveParams{}, result: memory.Entry{}},
		{name: "reload", params: NoParams{}, result: ReloadDiff{}},
		{name: "models.reload", params: NoParams{}, result: ModelReload{}},
		{name: "hooks.trust", params: NoParams{}, result: HooksTrusted{}},
		{name: "learn.scan", params: NoParams{}, result: learn.Run{}},
		{name: "learn.show", params: LearnParams{}, result: learn.Finding{}},
		{name: "learn.reject", params: LearnParams{}, result: learn.Finding{}},
		{name: "setup.check", params: NoParams{}, result: Setup{}},
		{name: "login.key", params: LoginKeyParams{}, result: LoginNote{}},
		{name: "login.logout", params: LogoutParams{}, result: LoginNote{}},
		{name: queryPrefix + "usage", params: NoParams{}, result: UsageAnswer{}},
		{name: queryPrefix + "doctor", params: NoParams{}, result: DoctorReport{}},
		{name: queryPrefix + "context", params: SessionParams{}, result: ContextReport{}},
		{name: "session.info", params: SessionParams{}, result: SessionInfo{}},
		{name: "session.find", params: SessionFindParams{}, result: SessionFind{}},
		{name: "session.trace", params: SessionParams{}, result: SessionTrace{}},
		{name: "mention.resolve", params: MentionParams{}, result: MentionResolved{}},
		{name: queryPrefix + "accounts", params: NoParams{}, result: Accounts{}},
		{name: queryPrefix + "session", params: SessionParams{}, result: SessionDetail{}},
		{name: queryPrefix + "usage.history", params: UsageHistoryParams{}, result: UsageHistoryReport{}},
		{name: queryPrefix + "limits", params: NoParams{}, result: LimitsReport{}},
		{name: queryPrefix + "skills", params: NoParams{}, result: SkillsReport{}},
		{name: queryPrefix + "ledger.summary", params: LedgerSummaryParams{}, result: LedgerSummary{}},
		{name: "learn.apply", params: LearnApplyParams{}, result: WriteReceipt{}},
		{name: statusMethod + ".list", params: StatusListParams{}, result: StatusList{}},
		{name: statusMethod + ".ack", params: StatusAckParams{}, result: Ack{}},
		{name: queryPrefix + "commands", params: NoParams{}, result: CommandList{}},
	}
	for _, write := range []string{"rules.add", "rules.off", "rules.remove", "rules.restore"} {
		methods = append(methods, method{name: write, params: RuleWriteParams{}, result: WriteReceipt{}})
	}
	for _, write := range []string{"agents.add", "agents.set", "agents.remove"} {
		methods = append(methods, method{name: write, params: AgentWriteParams{}, result: WriteReceipt{}})
	}
	return append(append(methods, boardyRequests()...), scratchRequests()...)
}

func capabilities() []string {
	return []string{"approvals", "questions", "resync", "shells", "queries", "cron", "rename", "list", "listed", "state", "set", "wires", "images", "lead", "unsteer", "sendNow", "run", "compact", "history", "ledger",
		"typed", "memory", "reload", "hooks", "learn", "docs", "changelog", "update", "doctor", "setup", "key", "logout", "info", "find", "trace", "accounts", "writes", "status", "mention", "boards", "side", "commands", "boardy", "scratch", "sessions"}
}

const queryPrefix = "query."

type CommandList struct {
	Commands []command.Command `json:"commands"`
}
