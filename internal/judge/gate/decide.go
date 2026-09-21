package gate

import (
	"tofu/internal/judge/jev"
	"tofu/internal/konst"
	"tofu/internal/transport"
)

func Decide(answers map[string]jev.Answer, r Rule) (Verdict, Reason, error) {
	riskAnswer, errRisk := questionAnswer(answers, r.RiskQuestion, jev.QuestionScore)
	approvalAnswer, errApproval := questionAnswer(answers, r.ApprovalQuestion, jev.QuestionNoul)
	userRequestedAnswer, errUserRequested := questionAnswer(answers, r.UserRequestedQuestion, jev.QuestionNoul)
	fromUntrustedAnswer, errFromUntrusted := questionAnswer(answers, r.FromUntrustedQuestion, jev.QuestionNoul)
	for _, err := range []error{errRisk, errApproval, errUserRequested, errFromUntrusted} {
		if err != nil {
			return "", Reason{}, err
		}
	}
	risk := riskAnswer.Score
	approval, userRequested, fromUntrusted := approvalAnswer.Noul, userRequestedAnswer.Noul, fromUntrustedAnswer.Noul

	base, comparison, deadBand := baseVerdict(risk, r.Thresholds)
	verdict, relaxedBy, blocked, ambiguous := applyAuthority(base, r, approval, userRequested, fromUntrusted)

	threshold := r.Thresholds.RiskAskAt
	if comparison == ComparisonRiskDenyAt {
		threshold = r.Thresholds.RiskDenyAt
	}
	reason := Reason{
		RuleVersion: r.RuleVersion,
		Question:    r.RiskQuestion,
		Comparison:  comparison,
		Threshold:   threshold,
		Value:       risk,
		DeadBand:    deadBand,
		RelaxedBy:   relaxedBy,
		Blocked:     blocked,
		Ambiguous:   ambiguous,
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

func applyAuthority(base Verdict, r Rule, approval, userRequested, fromUntrusted float64) (Verdict, string, bool, string) {
	band := konst.ThresholdDeadBand
	if fromUntrusted >= r.Thresholds.FromUntrustedBlockAt-band {
		return base, "", true, ""
	}
	hasHeadroom := base != VerdictAllow
	if userRequested > r.Thresholds.UserRequestedRelaxAt+band {
		if relaxed := base.relax(); relaxed != base {
			return relaxed, r.UserRequestedQuestion, false, ""
		}
	}
	ambiguous := ""
	if hasHeadroom && withinBand(userRequested, r.Thresholds.UserRequestedRelaxAt) {
		ambiguous = r.UserRequestedQuestion
	}
	if approval < r.Thresholds.ApprovalRelaxAt-band {
		if relaxed := base.relax(); relaxed != base {
			return relaxed, r.ApprovalQuestion, false, ""
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
		return jev.Answer{}, transport.Fail("gate.Decide", transport.KindInvalidAnswer, nil, "the rule needs a %s answer for %q and none was given", kind, question)
	}
	if a.Kind != kind {
		return jev.Answer{}, transport.Fail("gate.Decide", transport.KindInvalidAnswer, nil, "the rule needs %q to be a %s, found %s", question, kind, a.Kind)
	}
	return a, nil
}
