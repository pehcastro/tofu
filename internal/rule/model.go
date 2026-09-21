package rule

const (
	DomainDev     = "dev"
	DomainQA      = "qa"
	DomainGeneral = "general"
	DomainTools   = "tools"
)

const ThresholdKind = "threshold"

type Kind string

const (
	KindStructural Kind = "structural"
	KindDecision   Kind = "decision"
	KindHuman      Kind = "human"
	KindMeasured   Kind = "measured"
)

func (k Kind) valid() bool {
	switch k {
	case KindStructural, KindDecision, KindHuman, KindMeasured:
		return true
	}
	return false
}

type Mode string

const (
	ModeShadow   Mode = "shadow"
	ModeEnforced Mode = "enforced"
	ModeOff      Mode = "off"
)

func (m Mode) String() string {
	switch m {
	case ModeShadow:
		return "shadow"
	case ModeEnforced:
		return "enforced"
	case ModeOff:
		return "off"
	}
	panic("rule: unknown mode " + string(m))
}

type Exception string

const (
	ExceptionNone   Exception = ""
	ExceptionQuoted Exception = "quoted"
)

func (e Exception) valid() bool {
	switch e {
	case ExceptionNone, ExceptionQuoted:
		return true
	}
	return false
}

type Rule struct {
	ID           string
	Kind         Kind
	Domain       string
	Checker      string
	Measurement  string
	Source       string
	Evidence     string
	Mode         Mode
	ModeDeclared bool
	Except       Exception
	Notes        string
	File         string
}

type Finding struct {
	RuleID string
	Target string
	Detail string
}
