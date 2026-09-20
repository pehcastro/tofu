package cost

import (
	"fmt"
	"path/filepath"

	"tofu/internal/judge/jev"
	"tofu/internal/judge/policy"
)

const gatePolicyFile = "catalog/policy/tool_gate@1.yaml"

type Verdict string

const (
	Proceed Verdict = "proceed"
	Block   Verdict = "block"
	Refused Verdict = "refused"
)

func gatePolicy(root string) (policy.Policy, policy.Resolution, error) {
	pol, findings, err := policy.LintFile(filepath.Join(root, filepath.FromSlash(gatePolicyFile)))
	if err != nil {
		return policy.Policy{}, policy.Resolution{}, err
	}
	if len(findings) > 0 {
		return policy.Policy{}, policy.Resolution{}, fmt.Errorf("%s fails its own lint: %s", gatePolicyFile, findings[0])
	}
	pol.File = gatePolicyFile
	resolution := policy.Resolve(pol, policy.LockLookup{}, policy.Current{})
	return resolution.Policy, resolution, nil
}

func decide(answers map[string]jev.Answer, pol policy.Policy) (Verdict, policy.Reason, error) {
	verdict, reason, err := policy.Decide(answers, pol)
	if err != nil {
		return "", policy.Reason{}, err
	}
	if verdict == policy.VerdictAllow {
		return Proceed, reason, nil
	}
	return Block, reason, nil
}

type gateAnswers struct {
	Risk          float64
	Approval      float64
	UserRequested float64
	FromUntrusted float64
}

func answerMap(pol policy.Policy, answers gateAnswers) map[string]jev.Answer {
	return map[string]jev.Answer{
		pol.RiskQuestion:          {Kind: jev.QuestionScore, Score: answers.Risk},
		pol.ApprovalQuestion:      {Kind: jev.QuestionNoul, Noul: answers.Approval},
		pol.UserRequestedQuestion: {Kind: jev.QuestionNoul, Noul: answers.UserRequested},
		pol.FromUntrustedQuestion: {Kind: jev.QuestionNoul, Noul: answers.FromUntrusted},
	}
}

func rowAnswers(pol policy.Policy, row AnswerRow) (map[string]jev.Answer, bool) {
	if !row.AnswersKnown || row.Risk == nil || row.FromUntrusted == nil {
		return nil, false
	}
	return answerMap(pol, gateAnswers{
		Risk:          *row.Risk,
		Approval:      row.Approval,
		UserRequested: row.UserRequested,
		FromUntrusted: *row.FromUntrusted,
	}), true
}
