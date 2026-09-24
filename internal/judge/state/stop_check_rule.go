package state

import (
	"fmt"

	"tofu/internal/judge/gate"
	"tofu/internal/judge/question"
	shipped "tofu/library"
)

const StopCheckRuleRef = "stop_check@1"

const stopCheckSchema = "stop_check"

const (
	stopCheckRiskQuestion          = "stop_pressure"
	stopCheckApprovalQuestion      = "stalled"
	stopCheckUserRequestedQuestion = "work_remains"
	stopCheckFromUntrustedQuestion = "budget_exhausted"
)

func StopCheckRule() (gate.Rule, gate.Origin, error) {
	r, origin, err := gate.LoadPoint(shipped.Files(), StopCheckRuleRef, question.Set{}, "")
	if err != nil {
		return gate.Rule{}, "", err
	}
	if r.Schema != stopCheckSchema {
		return gate.Rule{}, "", fmt.Errorf("%s: schema is %q and stop_check reads %q", r.File, r.Schema, stopCheckSchema)
	}
	r.RiskQuestion = stopCheckRiskQuestion
	r.ApprovalQuestion = stopCheckApprovalQuestion
	r.UserRequestedQuestion = stopCheckUserRequestedQuestion
	r.FromUntrustedQuestion = stopCheckFromUntrustedQuestion
	r.Thresholds, err = stopCheckThresholds(r)
	if err != nil {
		return gate.Rule{}, "", err
	}
	return r, origin, nil
}

func stopCheckThresholds(r gate.Rule) (gate.Thresholds, error) {
	var t gate.Thresholds
	slots := map[string]*float64{
		"stop_pressure_ask_at":      &t.RiskAskAt,
		"stop_pressure_deny_at":     &t.RiskDenyAt,
		"work_remains_relax_at":     &t.UserRequestedRelaxAt,
		"stalled_relax_at":          &t.ApprovalRelaxAt,
		"budget_exhausted_block_at": &t.FromUntrustedBlockAt,
	}
	for name, dest := range slots {
		v, ok := r.ForeignThresholds[name]
		if !ok {
			return gate.Thresholds{}, fmt.Errorf("%s: thresholds has no %q", r.File, name)
		}
		*dest = v
	}
	return t, nil
}
