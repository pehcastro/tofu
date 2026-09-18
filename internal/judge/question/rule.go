package question

import "fmt"

type Rule string

const (
	RuleNoEscape          Rule = "no-escape-option"
	RuleUnknownEscape     Rule = "escape-option-not-in-the-set"
	RuleArithmetic        Rule = "asks-for-arithmetic"
	RuleTooManyOptions    Rule = "over-the-option-ceiling"
	RuleTooFewOptions     Rule = "fewer-than-two-options"
	RuleTooManyLevels     Rule = "over-the-level-ceiling"
	RuleTooFewLevels      Rule = "fewer-than-two-levels"
	RuleOversize          Rule = "over-the-request-ceiling"
	RuleFieldNotInState   Rule = "field-not-in-the-declared-state"
	RuleFieldUnparseable  Rule = "field-reference-not-understood"
	RuleFieldUnused       Rule = "declared-state-field-not-named"
	RuleNoWordingVersion  Rule = "no-questions-version"
	RuleWordingVersionOff Rule = "questions-version-is-not-the-file-version"
	RuleNoState           Rule = "no-declared-state-shape"
	RuleNoInstructions    Rule = "no-instructions"
	RuleNoQuestions       Rule = "no-questions"
)

type Finding struct {
	Rule     Rule
	Set      string
	Question string
	File     string
	Line     int
	Detail   string
}

func (f Finding) String() string {
	where := f.Set
	if f.Question != "" {
		where = f.Set + "." + f.Question
	}
	return fmt.Sprintf("%s:%d: %s: %s: %s", f.File, f.Line, where, f.Rule, f.Detail)
}
