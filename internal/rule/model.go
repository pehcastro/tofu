package rule

type Kind string

const (
	KindStructural Kind = "structural"
	KindDecision   Kind = "decision"
	KindHuman      Kind = "human"
)

func (k Kind) valid() bool {
	switch k {
	case KindStructural, KindDecision, KindHuman:
		return true
	}
	return false
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
	panic("rule: unknown mode " + string(m))
}

type Rule struct {
	ID           string
	Kind         Kind
	Checker      string
	Mode         Mode
	ModeDeclared bool
	Notes        string
	File         string
}

type Finding struct {
	RuleID string
	Target string
	Detail string
}
