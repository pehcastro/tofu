package gate

import (
	"encoding/json"
	"regexp"
	"strings"

	"tofu/internal/judge/jev"
)

type Arm string

const ArmRegex Arm = "regex"

const unavailablePrefix = "unavailable_"

type Fallback struct {
	Unavailable jev.Unavailable
	Arm         Arm
	ArmVerdict  Verdict
	Verdict     Verdict
}

func DecideUnavailable(err error, state []byte) Fallback {
	return Fallback{
		Unavailable: jev.Unavailability(err),
		Arm:         ArmRegex,
		ArmVerdict:  RegexArm(state),
		Verdict:     VerdictAsk,
	}
}

func (f Fallback) Comparison() Comparison {
	return Comparison(unavailablePrefix + f.Unavailable.String())
}

func (f Fallback) Reason(r Rule, mode Mode) Reason {
	return Reason{
		RuleVersion: r.RuleVersion,
		Question:    r.RiskQuestion,
		Comparison:  f.Comparison(),
		Mode:        mode,
	}
}

func (f Fallback) Sentence() string {
	return "the typed decision was not made: jev was unavailable (" + f.Unavailable.String() +
		"), the " + string(f.Arm) + " arm answered " + f.ArmVerdict.String() +
		", and a decision no threshold produced is recorded as " + f.Verdict.String() + ", never allow"
}

func IsUnavailable(comparison string) bool {
	return strings.HasPrefix(comparison, unavailablePrefix)
}

var (
	regexExfil = regexp.MustCompile(`(?i)curl\s+[^\n]*-d\s+@|curl\s+[^\n]*\|\s*sh`)
	regexForce = regexp.MustCompile(`(?i)push\s+.*--force|--force\b|force-push`)
	regexRM    = regexp.MustCompile(`(?i)\brm\s+-rf\b`)
	regexWord  = regexp.MustCompile(`(?i)\bforce\b`)
)

type regexState struct {
	Input struct {
		Command string `json:"command"`
	} `json:"input"`
	Context struct {
		UserRecentMessages []string `json:"user_recent_messages"`
	} `json:"context"`
}

func RegexArm(state []byte) Verdict {
	var read regexState
	if err := json.Unmarshal(state, &read); err != nil {
		return VerdictDeny
	}
	command := read.Input.Command
	userMessage := ""
	if len(read.Context.UserRecentMessages) > 0 {
		userMessage = read.Context.UserRecentMessages[0]
	}
	switch {
	case regexExfil.MatchString(command):
		return VerdictDeny
	case regexForce.MatchString(command):
		if regexWord.MatchString(userMessage) {
			return VerdictAllow
		}
		return VerdictDeny
	case regexRM.MatchString(command):
		return VerdictDeny
	default:
		return VerdictAllow
	}
}
