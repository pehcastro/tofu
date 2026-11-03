package harness

import "time"

type Arm string

const (
	ArmClaude Arm = "claude"
	ArmCodex  Arm = "codex"
	ArmTofu   Arm = "tofu"
)

type Effort string

const EffortMedium Effort = "medium"

type CredentialKind string

const (
	CredentialKindKey          CredentialKind = "key"
	CredentialKindSubscription CredentialKind = "subscription"
)

type RunMeta struct {
	Arm            Arm
	Task           string
	Version        int
	Run            int
	CLIVersion     string
	CredentialKind CredentialKind
	Commit         string
	Setup          Setup
	Seed           Seed
	Effort         Effort
}

type EndReason string

const (
	EndReasonDone      EndReason = "done"
	EndReasonWallClock EndReason = "wall_clock_cap"
	EndReasonTurnCap   EndReason = "turn_cap"
	EndReasonTruncated EndReason = "output_truncated"
	EndReasonCrash     EndReason = "crash"
)

type ToolCalls struct {
	Read    int64 `json:"read"`
	Write   int64 `json:"write"`
	Edit    int64 `json:"edit"`
	Search  int64 `json:"search"`
	Shell   int64 `json:"shell"`
	Other   int64 `json:"other"`
	Failed  int64 `json:"failed"`
	Retried int64 `json:"retried"`
}

type GateResult struct {
	Name   string     `json:"name"`
	Status GateStatus `json:"status"`
	Reason string     `json:"reason"`
}

type ChecklistResult struct {
	Item   string `json:"item"`
	Passed bool   `json:"passed"`
}

type Row struct {
	Arm            Arm               `json:"arm"`
	Task           string            `json:"task"`
	Setup          Setup             `json:"setup"`
	Seed           Seed              `json:"seed"`
	Effort         Effort            `json:"effort"`
	Version        int               `json:"version"`
	Run            int               `json:"run"`
	Start          time.Time         `json:"start"`
	End            time.Time         `json:"end"`
	WallClockMS    int64             `json:"wall_clock_ms"`
	Model          string            `json:"model"`
	CLIVersion     string            `json:"cli_version"`
	CredentialKind CredentialKind    `json:"credential_kind"`
	BilledInput    int64             `json:"billed_input_tokens"`
	BilledOutput   int64             `json:"billed_output_tokens"`
	ModelDollars   *float64          `json:"model_dollars"`
	JudgeDollars   float64           `json:"judge_dollars"`
	Turns          int64             `json:"turns"`
	ToolCalls      ToolCalls         `json:"tool_calls"`
	EndReason      EndReason         `json:"end_reason"`
	Commit         string            `json:"commit"`
	Gates          []GateResult      `json:"gates"`
	Checklist      []ChecklistResult `json:"checklist"`
}
