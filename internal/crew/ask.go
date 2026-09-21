package crew

import "fmt"

type AskState struct {
	Path          string `json:"path"`
	InOwns        bool   `json:"in_owns"`
	Answered      bool   `json:"answered_by_spec_or_ticket"`
	Precedent     bool   `json:"precedent_exists"`
	Reversibility string `json:"reversibility"`
}

const (
	ActionProceed = "proceed"
	ActionAskNow  = "ask_now"
	ActionDefer   = "defer_to_end"
)

const (
	ActionQuestion     = "action"
	DeterminedQuestion = "determined"
)

type UnknownActionError struct {
	Action string
}

func (e UnknownActionError) Error() string {
	return fmt.Sprintf("crew: %q is not one of %s, %s, %s", e.Action, ActionProceed, ActionAskNow, ActionDefer)
}

type GateVerdict struct {
	Action          string
	Effective       string
	Determined      float64
	DeterminedLowAt float64
}

func DecideAsk(action string, determined, determinedLowAt float64) (GateVerdict, error) {
	switch action {
	case ActionProceed, ActionAskNow, ActionDefer:
	default:
		return GateVerdict{}, UnknownActionError{Action: action}
	}
	effective := action
	if determined >= determinedLowAt {
		effective = ActionProceed
	}
	return GateVerdict{Action: action, Effective: effective, Determined: determined, DeterminedLowAt: determinedLowAt}, nil
}

func (v GateVerdict) Question(state AskState) (Question, bool) {
	var kind Kind
	switch v.Effective {
	case ActionProceed:
		return Question{}, false
	case ActionAskNow:
		kind = Blocking
	case ActionDefer:
		kind = Deferred
	default:
		panic("crew: unknown ask action " + v.Effective)
	}
	return Question{
		Kind:  kind,
		Where: state.Path,
		Ask: fmt.Sprintf("shadow ask gate: %s, determined %.2f under the %.2f margin",
			v.Action, v.Determined, v.DeterminedLowAt),
		Default: "proceeded on the worker's own reading; the gate only recorded a shadow verdict",
	}, true
}
