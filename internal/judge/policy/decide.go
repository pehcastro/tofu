package policy

import (
	"tofu/internal/judge/jev"
	"tofu/internal/konst"
	"tofu/internal/transport"
)

func Decide(answers map[string]jev.Answer, pol Policy) (Verdict, Reason, error) {
	riskAnswer, errRisk := questionAnswer(answers, pol.RiskQuestion, jev.QuestionScore)
	approvalAnswer, errApproval := questionAnswer(answers, pol.ApprovalQuestion, jev.QuestionNoul)
	userRequestedAnswer, errUserRequested := questionAnswer(answers, pol.UserRequestedQuestion, jev.QuestionNoul)
	fromUntrustedAnswer, errFromUntrusted := questionAnswer(answers, pol.FromUntrustedQuestion, jev.QuestionNoul)
	for _, err := range []error{errRisk, errApproval, errUserRequested, errFromUntrusted} {
		if err != nil {
			return "", Reason{}, err
		}
	}
	risk := riskAnswer.Score
	approval, userRequested, fromUntrusted := approvalAnswer.Noul, userRequestedAnswer.Noul, fromUntrustedAnswer.Noul

	base, comparison, deadBand := baseVerdict(risk, pol.Thresholds)
	verdict, relaxedBy, blocked, ambiguous := applyAuthority(base, pol, approval, userRequested, fromUntrusted)

	threshold := pol.Thresholds.RiskAskAt
	if comparison == ComparisonRiskDenyAt {
		threshold = pol.Thresholds.RiskDenyAt
	}
	reason := Reason{
		PolicyVersion: pol.PolicyVersion,
		Question:      pol.RiskQuestion,
		Comparison:    comparison,
		Threshold:     threshold,
		Value:         risk,
		DeadBand:      deadBand,
		RelaxedBy:     relaxedBy,
		Blocked:       blocked,
		Ambiguous:     ambiguous,
	}
	return verdict, reason, nil
}

func baseVerdict(risk float64, t Thresholds) (Verdict, Comparison, bool) {
	if withinBand(risk, t.RiskDenyAt) {
		return VerdictAsk, ComparisonRiskDenyAt, true
	}
	if risk > t.RiskDenyAt {
		return VerdictDeny, ComparisonRiskDenyAt, false
	}
	if withinBand(risk, t.RiskAskAt) {
		return VerdictAsk, ComparisonRiskAskAt, true
	}
	if risk > t.RiskAskAt {
		return VerdictAsk, ComparisonRiskAskAt, false
	}
	return VerdictAllow, ComparisonRiskAskAt, false
}

func applyAuthority(base Verdict, pol Policy, approval, userRequested, fromUntrusted float64) (Verdict, string, bool, string) {
	band := konst.ThresholdDeadBand
	if fromUntrusted >= pol.Thresholds.FromUntrustedBlockAt-band {
		return base, "", true, ""
	}
	hasHeadroom := base != VerdictAllow
	if userRequested > pol.Thresholds.UserRequestedRelaxAt+band {
		if relaxed := base.relax(); relaxed != base {
			return relaxed, pol.UserRequestedQuestion, false, ""
		}
	}
	ambiguous := ""
	if hasHeadroom && withinBand(userRequested, pol.Thresholds.UserRequestedRelaxAt) {
		ambiguous = pol.UserRequestedQuestion
	}
	if approval < pol.Thresholds.ApprovalRelaxAt-band {
		if relaxed := base.relax(); relaxed != base {
			return relaxed, pol.ApprovalQuestion, false, ""
		}
	}
	return base, "", false, ambiguous
}

func withinBand(value, threshold float64) bool {
	d := value - threshold
	if d < 0 {
		d = -d
	}
	return d <= konst.ThresholdDeadBand
}

func questionAnswer(answers map[string]jev.Answer, question string, kind jev.QuestionKind) (jev.Answer, error) {
	a, ok := answers[question]
	if !ok {
		return jev.Answer{}, transport.Fail("policy.Decide", transport.KindInvalidAnswer, nil, "the policy needs a %s answer for %q and none was given", kind, question)
	}
	if a.Kind != kind {
		return jev.Answer{}, transport.Fail("policy.Decide", transport.KindInvalidAnswer, nil, "the policy needs %q to be a %s, found %s", question, kind, a.Kind)
	}
	return a, nil
}
