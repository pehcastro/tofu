package gate

import (
	"fmt"
	"path/filepath"

	"tofu/internal/judge/question"
)

type Check string

const (
	CheckQuestionMissing   Check = "question-not-in-the-set"
	CheckQuestionWrongKind Check = "question-is-the-wrong-kind"
	CheckSetMismatch       Check = "rule-names-a-different-question-set"
	CheckVersionMismatch   Check = "rule-and-question-versions-disagree"
)

type Finding struct {
	Check  Check
	Rule   string
	Field  string
	File   string
	Detail string
}

func (f Finding) String() string {
	where := f.Rule
	if f.Field != "" {
		where = f.Rule + "." + f.Field
	}
	return fmt.Sprintf("%s: %s: %s: %s", f.File, where, f.Check, f.Detail)
}

func Lint(r Rule, set question.Set) []Finding {
	var out []Finding
	add := func(check Check, field, detail string) {
		out = append(out, Finding{Check: check, Rule: r.Name, Field: field, File: r.File, Detail: detail})
	}
	if r.Questions != set.Name {
		add(CheckSetMismatch, "questions", fmt.Sprintf("the rule names the question set %q and the set loaded is %q", r.Questions, set.Name))
	}
	if r.QuestionsVersion != set.QuestionsVersion {
		add(CheckVersionMismatch, "questions_version", fmt.Sprintf("the rule expects questions_version %d and the set declares %d", r.QuestionsVersion, set.QuestionsVersion))
	}
	lintQuestion(&out, r, set, "risk_question", r.RiskQuestion, question.KindScore)
	lintQuestion(&out, r, set, "approval_question", r.ApprovalQuestion, question.KindNoul)
	lintQuestion(&out, r, set, "user_requested_question", r.UserRequestedQuestion, question.KindNoul)
	lintQuestion(&out, r, set, "from_untrusted_question", r.FromUntrustedQuestion, question.KindNoul)
	return out
}

func lintQuestion(out *[]Finding, r Rule, set question.Set, field, name string, want question.Kind) {
	add := func(check Check, detail string) {
		*out = append(*out, Finding{Check: check, Rule: r.Name, Field: field, File: r.File, Detail: detail})
	}
	q, ok := set.Question(name)
	if !ok {
		add(CheckQuestionMissing, fmt.Sprintf("%s names %q and the question set %q has no such question", field, name, set.Name))
		return
	}
	if q.Kind != want {
		add(CheckQuestionWrongKind, fmt.Sprintf("%s names %q, a %s, and the rule reads it as a %s", field, name, q.Kind, want))
	}
}

func LintFile(rulePath, libraryDir string) (Rule, []Finding, error) {
	r, err := Load(rulePath)
	if err != nil {
		return Rule{}, nil, err
	}
	if r.Schema != SchemaGate {
		return r, nil, nil
	}
	questionsPath := filepath.Join(libraryDir, "questions", fmt.Sprintf("%s@%d.yaml", r.Questions, r.QuestionsVersion))
	set, err := question.Load(questionsPath)
	if err != nil {
		return r, nil, err
	}
	return r, Lint(r, set), nil
}
