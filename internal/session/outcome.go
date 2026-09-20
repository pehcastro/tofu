package session

import (
	"encoding/json"
	"fmt"
)

type Outcome int

const (
	OutcomeUnset Outcome = iota
	OutcomeStopped
	OutcomeStepCap
	OutcomeRetiredCostCap
	OutcomeRetiredWallClockCap
	OutcomeDecisionCap
	OutcomeForked
	OutcomeError
	OutcomeTruncated
	outcomeCount
)

func (o Outcome) String() string {
	switch o {
	case OutcomeUnset:
		return "unset"
	case OutcomeStopped:
		return "stopped"
	case OutcomeStepCap:
		return "step_cap"
	case OutcomeRetiredCostCap:
		return "cost_cap"
	case OutcomeRetiredWallClockCap:
		return "wall_clock_cap"
	case OutcomeDecisionCap:
		return "decision_cap"
	case OutcomeForked:
		return "forked"
	case OutcomeError:
		return "error"
	case OutcomeTruncated:
		return "truncated"
	}
	panic(fmt.Sprintf("session: outcome %d has no name", int(o)))
}

func (o Outcome) MarshalJSON() ([]byte, error) {
	return json.Marshal(o.String())
}

func (o *Outcome) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return err
	}
	for candidate := OutcomeUnset; candidate < outcomeCount; candidate++ {
		if candidate.String() == text {
			*o = candidate
			return nil
		}
	}
	return fmt.Errorf("session: row carries unknown outcome %q", text)
}
