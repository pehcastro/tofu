package cost

import (
	"fmt"
	"path/filepath"

	"tofu/internal/judge/gate"
	"tofu/internal/judge/jev"
)

const gateRuleFile = "catalog/general/rules/tool_gate@1.yaml"

type Verdict string

const (
	Proceed Verdict = "proceed"
	Block   Verdict = "block"
	Refused Verdict = "refused"
)

func gateRule(root string) (gate.Rule, gate.Resolution, error) {
	pol, findings, err := gate.LintFile(filepath.Join(root, filepath.FromSlash(gateRuleFile)), filepath.Join(root, "catalog"))
	if err != nil {
		return gate.Rule{}, gate.Resolution{}, err
	}
	if len(findings) > 0 {
		return gate.Rule{}, gate.Resolution{}, fmt.Errorf("%s fails its own lint: %s", gateRuleFile, findings[0])
	}
	pol.File = gateRuleFile
	resolution := gate.Resolve(pol, gate.LockLookup{}, gate.Current{})
	return resolution.Rule, resolution, nil
}

func decide(answers map[string]jev.Answer, pol gate.Rule) (Verdict, gate.Reason, error) {
	verdict, reason, err := gate.Decide(answers, pol)
	if err != nil {
		return "", gate.Reason{}, err
	}
	if verdict == gate.VerdictAllow {
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

func answerMap(pol gate.Rule, answers gateAnswers) map[string]jev.Answer {
	return map[string]jev.Answer{
		pol.RiskQuestion:          {Kind: jev.QuestionScore, Score: answers.Risk},
		pol.ApprovalQuestion:      {Kind: jev.QuestionNoul, Noul: answers.Approval},
		pol.UserRequestedQuestion: {Kind: jev.QuestionNoul, Noul: answers.UserRequested},
		pol.FromUntrustedQuestion: {Kind: jev.QuestionNoul, Noul: answers.FromUntrusted},
	}
}

func rowAnswers(pol gate.Rule, row AnswerRow) (map[string]jev.Answer, bool) {
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
