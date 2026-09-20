package state

import (
	"fmt"

	catalogpolicy "tofu/catalog/policy"
	"tofu/internal/judge/policy"
	"tofu/internal/judge/question"
)

const StopCheckPolicyRef = "stop_check@1"

const stopCheckSchema = "stop_check"

const (
	stopCheckRiskQuestion          = "stop_pressure"
	stopCheckApprovalQuestion      = "stalled"
	stopCheckUserRequestedQuestion = "work_remains"
	stopCheckFromUntrustedQuestion = "budget_exhausted"
)

func StopCheckPolicy() (policy.Policy, policy.Origin, error) {
	pol, origin, err := policy.LoadPoint(catalogpolicy.Files(), StopCheckPolicyRef, question.Set{})
	if err != nil {
		return policy.Policy{}, "", err
	}
	if pol.Schema != stopCheckSchema {
		return policy.Policy{}, "", fmt.Errorf("%s: schema is %q and stop_check reads %q", pol.File, pol.Schema, stopCheckSchema)
	}
	pol.RiskQuestion = stopCheckRiskQuestion
	pol.ApprovalQuestion = stopCheckApprovalQuestion
	pol.UserRequestedQuestion = stopCheckUserRequestedQuestion
	pol.FromUntrustedQuestion = stopCheckFromUntrustedQuestion
	pol.Thresholds, err = stopCheckThresholds(pol)
	if err != nil {
		return policy.Policy{}, "", err
	}
	return pol, origin, nil
}

func stopCheckThresholds(pol policy.Policy) (policy.Thresholds, error) {
	var t policy.Thresholds
	slots := map[string]*float64{
		"stop_pressure_ask_at":      &t.RiskAskAt,
		"stop_pressure_deny_at":     &t.RiskDenyAt,
		"work_remains_relax_at":     &t.UserRequestedRelaxAt,
		"stalled_relax_at":          &t.ApprovalRelaxAt,
		"budget_exhausted_block_at": &t.FromUntrustedBlockAt,
	}
	for name, dest := range slots {
		v, ok := pol.ForeignThresholds[name]
		if !ok {
			return policy.Thresholds{}, fmt.Errorf("%s: thresholds has no %q", pol.File, name)
		}
		*dest = v
	}
	return t, nil
}
