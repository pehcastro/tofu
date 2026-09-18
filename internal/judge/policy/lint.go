package policy

import (
	"fmt"
	"path/filepath"

	"boji/internal/judge/question"
)

type Rule string

const (
	RuleQuestionMissing   Rule = "question-not-in-the-set"
	RuleQuestionWrongKind Rule = "question-is-the-wrong-kind"
	RuleSetMismatch       Rule = "policy-names-a-different-question-set"
	RuleVersionMismatch   Rule = "policy-and-question-versions-disagree"
)

type Finding struct {
	Rule   Rule
	Policy string
	Field  string
	File   string
	Detail string
}

func (f Finding) String() string {
	where := f.Policy
	if f.Field != "" {
		where = f.Policy + "." + f.Field
	}
	return fmt.Sprintf("%s: %s: %s: %s", f.File, where, f.Rule, f.Detail)
}

func Lint(pol Policy, set question.Set) []Finding {
	var out []Finding
	add := func(rule Rule, field, detail string) {
		out = append(out, Finding{Rule: rule, Policy: pol.Name, Field: field, File: pol.File, Detail: detail})
	}
	if pol.Questions != set.Name {
		add(RuleSetMismatch, "questions", fmt.Sprintf("the policy names the question set %q and the set loaded is %q", pol.Questions, set.Name))
	}
	if pol.QuestionsVersion != set.QuestionsVersion {
		add(RuleVersionMismatch, "questions_version", fmt.Sprintf("the policy expects questions_version %d and the set declares %d", pol.QuestionsVersion, set.QuestionsVersion))
	}
	lintQuestion(&out, pol, set, "risk_question", pol.RiskQuestion, question.KindScore)
	lintQuestion(&out, pol, set, "approval_question", pol.ApprovalQuestion, question.KindNoul)
	lintQuestion(&out, pol, set, "user_requested_question", pol.UserRequestedQuestion, question.KindNoul)
	lintQuestion(&out, pol, set, "from_untrusted_question", pol.FromUntrustedQuestion, question.KindNoul)
	return out
}

func lintQuestion(out *[]Finding, pol Policy, set question.Set, field, name string, want question.Kind) {
	add := func(rule Rule, detail string) {
		*out = append(*out, Finding{Rule: rule, Policy: pol.Name, Field: field, File: pol.File, Detail: detail})
	}
	q, ok := set.Question(name)
	if !ok {
		add(RuleQuestionMissing, fmt.Sprintf("%s names %q and the question set %q has no such question", field, name, set.Name))
		return
	}
	if q.Kind != want {
		add(RuleQuestionWrongKind, fmt.Sprintf("%s names %q, a %s, and the policy reads it as a %s", field, name, q.Kind, want))
	}
}

func LintFile(policyPath string) (Policy, []Finding, error) {
	pol, err := Load(policyPath)
	if err != nil {
		return Policy{}, nil, err
	}
	catalogDir := filepath.Dir(filepath.Dir(policyPath))
	questionsPath := filepath.Join(catalogDir, "questions", fmt.Sprintf("%s@%d.yaml", pol.Questions, pol.QuestionsVersion))
	set, err := question.Load(questionsPath)
	if err != nil {
		return pol, nil, err
	}
	return pol, Lint(pol, set), nil
}
