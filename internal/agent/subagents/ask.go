package subagent

import "fmt"

type AskState struct {
	Asker         Asker       `json:"asker"`
	Question      string      `json:"question"`
	Options       []AskOption `json:"options"`
	Recommended   string      `json:"recommended"`
	Path          string      `json:"path"`
	InOwns        bool        `json:"in_owns"`
	Answered      bool        `json:"answered_by_spec_or_ticket"`
	Precedent     bool        `json:"precedent_exists"`
	Reversibility string      `json:"reversibility"`
}

type Asker string

const AskerLead Asker = "lead"

type AskOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
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
	return fmt.Sprintf("subagent: %q is not one of %s, %s, %s", e.Action, ActionProceed, ActionAskNow, ActionDefer)
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
