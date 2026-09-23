package turn

import (
	"context"
	"encoding/json"
	"strconv"

	"tofu/internal/judge/ledger"
)

type GateRequest struct {
	TurnID string
	Task   string
	Tool   string
	Args   json.RawMessage
}

type GateDecision struct {
	ID      string
	Verdict ledger.Verdict
	Answers []ledger.Answer
	Reason  *ledger.Reason
}

type Gate interface {
	Decide(ctx context.Context, request GateRequest) (GateDecision, error)
}

type GateMode int

const (
	GateShadow GateMode = iota
	GateEnforce
)

func AllGateModes() []GateMode {
	return []GateMode{GateShadow, GateEnforce}
}

func (m GateMode) String() string {
	switch m {
	case GateShadow:
		return "shadow"
	case GateEnforce:
		return "enforce"
	}
	panic("turn: unknown gate mode")
}

type PersonAnswer int

const (
	PersonDenied PersonAnswer = iota
	PersonAllowedOnce
	PersonAlwaysHere
)

func AllPersonAnswers() []PersonAnswer {
	return []PersonAnswer{PersonDenied, PersonAllowedOnce, PersonAlwaysHere}
}

func (a PersonAnswer) allows() bool {
	switch a {
	case PersonDenied:
		return false
	case PersonAllowedOnce, PersonAlwaysHere:
		return true
	}
	panic("turn: unknown person answer")
}

const OutcomeKindGateAnswer = "gate-answer"

func (a PersonAnswer) Outcome() ledger.Outcome {
	switch a {
	case PersonDenied:
		return ledger.Outcome{Kind: OutcomeKindGateAnswer, Detail: "deny"}
	case PersonAllowedOnce, PersonAlwaysHere:
		return ledger.Outcome{Kind: OutcomeKindGateAnswer, Detail: "allow"}
	}
	panic("turn: unknown person answer")
}

type Person func(ctx context.Context, request GateRequest, decision GateDecision) (PersonAnswer, error)

const (
	refusedHead = "the tool gate refused this call under an enforced policy: "
	refusedTail = ". nothing ran and nothing changed. find another way to do the task, or say why this call is needed and stop."
)

func gateRefusal(ctx context.Context, person Person, request GateRequest, decision GateDecision, gateErr string) string {
	why := refusedWhy(ctx, person, request, decision, gateErr)
	if why == "" {
		return ""
	}
	return refusedHead + why + refusedTail
}

func refusedWhy(ctx context.Context, person Person, request GateRequest, decision GateDecision, gateErr string) string {
	if gateErr != "" {
		return "the gate could not answer, and a check that cannot run refuses: " + gateErr
	}
	switch decision.Verdict {
	case ledger.VerdictUnset, ledger.VerdictAllow:
		return ""
	case ledger.VerdictDeny:
		return "the verdict is deny" + standing(decision.Reason)
	case ledger.VerdictAsk:
		if person == nil {
			return "the verdict is ask" + standing(decision.Reason) + ", and no person was available to answer"
		}
		answer, err := person(ctx, request, decision)
		switch {
		case err != nil:
			return "the verdict is ask and the person could not be asked: " + err.Error()
		case answer.allows():
			return ""
		}
		return "the verdict is ask" + standing(decision.Reason) + ", and the person did not allow it"
	}
	panic("turn: unknown verdict " + string(decision.Verdict))
}

func standing(reason *ledger.Reason) string {
	if reason == nil || reason.Question == "" {
		return ""
	}
	where := " is over "
	switch {
	case reason.DeadBand:
		where = " is within the dead band of "
	case reason.Value <= reason.Threshold:
		where = " is under "
	}
	return ", " + reason.Question + " " + number(reason.Value) + where + reason.Comparison + " " + number(reason.Threshold)
}

func number(value float64) string {
	return strconv.FormatFloat(value, 'f', 2, 64)
}
