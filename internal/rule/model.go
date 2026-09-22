package rule

import (
	"slices"
	"strings"
)

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

type Concern string

const (
	ConcernOutputShape       Concern = "output_shape"
	ConcernSafety            Concern = "safety"
	ConcernEnvironment       Concern = "environment"
	ConcernToolGuidance      Concern = "tool_guidance"
	ConcernFormatContract    Concern = "format_contract"
	ConcernCodeRules         Concern = "code_rules"
	ConcernProcessDiscipline Concern = "process_discipline"
	ConcernDomainKnowledge   Concern = "domain_knowledge"
	ConcernTaskShaping       Concern = "task_shaping"
	ConcernIdentity          Concern = "identity"
)

func neverConditionalConcerns() []Concern {
	return []Concern{ConcernOutputShape, ConcernSafety, ConcernEnvironment, ConcernToolGuidance, ConcernFormatContract}
}

func conditionalConcerns() []Concern {
	return []Concern{ConcernCodeRules, ConcernProcessDiscipline, ConcernDomainKnowledge, ConcernTaskShaping, ConcernIdentity}
}

func allConcerns() []Concern {
	return append(neverConditionalConcerns(), conditionalConcerns()...)
}

func (c Concern) neverConditional() bool { return slices.Contains(neverConditionalConcerns(), c) }

func (c Concern) valid() bool { return slices.Contains(allConcerns(), c) }

func concernNames(of []Concern) string {
	names := make([]string, len(of))
	for i, one := range of {
		names[i] = string(one)
	}
	return strings.Join(names, ", ")
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
	Concern      Concern
	Domain       string
	Checker      string
	Measurement  string
	Source       string
	Evidence     string
	Mode         Mode
	ModeDeclared bool
	Except       Exception
	Trigger      Trigger
	Notes        string
	File         string
}

type Finding struct {
	RuleID string
	Target string
	Detail string
}
