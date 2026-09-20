package policy

import "fmt"

type Verdict string

const (
	VerdictAllow Verdict = "allow"
	VerdictAsk   Verdict = "ask"
	VerdictDeny  Verdict = "deny"
)

func (v Verdict) String() string {
	switch v {
	case VerdictAllow:
		return "allow"
	case VerdictAsk:
		return "ask"
	case VerdictDeny:
		return "deny"
	}
	panic("policy: unknown verdict " + string(v))
}

func (v Verdict) relax() Verdict {
	switch v {
	case VerdictDeny:
		return VerdictAsk
	case VerdictAsk:
		return VerdictAllow
	case VerdictAllow:
		return VerdictAllow
	}
	panic("policy: unknown verdict " + string(v))
}

type Mode string

const (
	ModeShadow   Mode = "shadow"
	ModeEnforced Mode = "enforced"
)

func (m Mode) String() string {
	switch m {
	case ModeShadow:
		return "shadow"
	case ModeEnforced:
		return "enforced"
	}
	panic("policy: unknown mode " + string(m))
}

type Comparison string

const (
	ComparisonRiskAskAt  Comparison = "risk_ask_at"
	ComparisonRiskDenyAt Comparison = "risk_deny_at"
)

type Thresholds struct {
	RiskAskAt            float64
	RiskDenyAt           float64
	UserRequestedRelaxAt float64
	ApprovalRelaxAt      float64
	FromUntrustedBlockAt float64
}

func (t Thresholds) String() string {
	return fmt.Sprintf("risk_ask_at=%g risk_deny_at=%g user_requested_relax_at=%g approval_relax_at=%g from_untrusted_block_at=%g",
		t.RiskAskAt, t.RiskDenyAt, t.UserRequestedRelaxAt, t.ApprovalRelaxAt, t.FromUntrustedBlockAt)
}

type Policy struct {
	Name                  string
	PolicyVersion         int
	Questions             string
	QuestionsVersion      int
	RiskQuestion          string
	ApprovalQuestion      string
	UserRequestedQuestion string
	FromUntrustedQuestion string
	Thresholds            Thresholds
	Mode                  Mode
	ModeDeclared          bool
	SampleFloor           int
	Notes                 string
	File                  string
}

type Reason struct {
	PolicyVersion int
	Question      string
	Comparison    Comparison
	Threshold     float64
	Value         float64
	DeadBand      bool
	RelaxedBy     string
	Blocked       bool
	Ambiguous     string
	Mode          Mode
}
