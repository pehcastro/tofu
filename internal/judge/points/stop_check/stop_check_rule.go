package stopcheck

import (
	"fmt"

	"tofu/internal/judge/gate"
	"tofu/internal/judge/questions"
	shipped "tofu/library"
)

const StopCheckRuleRef = "stop_check@1"

const stopCheckSchema = "stop_check"

const (
	stopCheckRiskQuestion          = "stop_pressure"
	stopCheckApprovalQuestion      = "stalled"
	stopCheckUserRequestedQuestion = "work_remains"
	stopCheckFromUntrustedQuestion = "budget_exhausted"
	stopCheckAskAt                 = stopCheckRiskQuestion + "_ask_at"
	stopCheckDenyAt                = stopCheckRiskQuestion + "_deny_at"
)

func StopCheckRule() (gate.Rule, gate.Origin, error) {
	r, origin, err := gate.LoadPoint(shipped.Files(), StopCheckRuleRef, questions.Set{}, "")
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
	r.RiskNames = gate.RiskNames{AskAt: stopCheckAskAt, DenyAt: stopCheckDenyAt}
	r.Thresholds, err = stopCheckThresholds(r)
	if err != nil {
		return gate.Rule{}, "", err
	}
	return r, origin, nil
}

func StopCheckThresholdSlots(t *gate.Thresholds) map[string]*float64 {
	return map[string]*float64{
		stopCheckAskAt:              &t.RiskAskAt,
		stopCheckDenyAt:             &t.RiskDenyAt,
		"work_remains_relax_at":     &t.UserRequestedRelaxAt,
		"stalled_relax_at":          &t.ApprovalRelaxAt,
		"budget_exhausted_block_at": &t.FromUntrustedBlockAt,
	}
}

func stopCheckThresholds(r gate.Rule) (gate.Thresholds, error) {
	var t gate.Thresholds
	for name, dest := range StopCheckThresholdSlots(&t) {
		v, ok := r.ForeignThresholds[name]
		if !ok {
			return gate.Thresholds{}, fmt.Errorf("%s: thresholds has no %q", r.File, name)
		}
		*dest = v
	}
	return t, nil
}
