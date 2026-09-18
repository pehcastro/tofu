package question

import (
	"fmt"
	"strings"

	"boji/internal/judge/jev"
	"boji/internal/konst"
)

func DefaultCaps() jev.WireCaps {
	return jev.WireCaps{
		MaxChoiceOptions: konst.ChoiceCeiling,
		MaxStateTokens:   konst.JudgeStateTokenCeiling,
		MaxRequestTokens: konst.JudgeRequestTokenCeiling,
		MaxScoreLevels:   konst.JudgeScoreLevelCeiling,
	}
}

func LintFile(path string, caps jev.WireCaps) ([]Finding, error) {
	set, err := Load(path)
	if err != nil {
		return nil, err
	}
	return Lint(set, caps), nil
}

func Lint(set Set, caps jev.WireCaps) []Finding {
	var out []Finding
	add := func(rule Rule, question string, line int, detail string) {
		out = append(out, Finding{Rule: rule, Set: set.Name, Question: question, File: set.File, Line: line, Detail: detail})
	}
	if set.QuestionsVersion == 0 {
		add(RuleNoWordingVersion, "", 1, "the set declares no questions_version, so no answer can be keyed to a wording")
	} else if set.QuestionsVersion != set.Version {
		add(RuleWordingVersionOff, "", 1, fmt.Sprintf("questions_version is %d and the file is version %d", set.QuestionsVersion, set.Version))
	}
	if len(set.State) == 0 {
		add(RuleNoState, "", 1, "the set declares no state shape, and state shape is part of the question")
	}
	if len(set.Questions) == 0 {
		add(RuleNoQuestions, "", 1, "the set asks nothing")
	}
	stateTokens, longestQuestionTokens, totalTokens := requestTokens(set)
	if bound := caps.MaxStateTokens; bound > 0 && stateTokens+longestQuestionTokens > bound {
		add(RuleOversize, "", 1, fmt.Sprintf(
			"the declared state is about %d tokens and the longest question about %d, together over the route's %d token ceiling for state plus the longest question",
			stateTokens, longestQuestionTokens, bound))
	}
	if bound := caps.MaxRequestTokens; bound > 0 && totalTokens > bound {
		add(RuleOversize, "", 1, fmt.Sprintf("the wording alone is about %d tokens and the route accepts at most %d for the whole request", totalTokens, bound))
	}
	if !hasBlanketQuestion(set.Questions) {
		for _, field := range unusedFields(set) {
			add(RuleFieldUnused, "", 1, fmt.Sprintf("the state declares `%s` and no question names it", field))
		}
	}
	for _, q := range set.Questions {
		out = append(out, lintQuestion(set, q, caps)...)
	}
	return out
}

func lintQuestion(set Set, q Question, caps jev.WireCaps) []Finding {
	var out []Finding
	add := func(rule Rule, detail string) {
		out = append(out, Finding{Rule: rule, Set: set.Name, Question: q.Name, File: q.File, Line: q.Line, Detail: detail})
	}
	if strings.TrimSpace(q.Instructions) == "" {
		add(RuleNoInstructions, "the question has no instructions")
	}
	for _, phrase := range arithmeticIn(q.Words()) {
		add(RuleArithmetic, fmt.Sprintf("the wording contains %q, and counting, arithmetic and date comparison are code work", phrase))
	}
	unknown, unparseable := fieldFindings(q.Words(), set.State)
	for _, ref := range unknown {
		add(RuleFieldNotInState, fmt.Sprintf("the wording names `%s` and the declared state has no such field", ref))
	}
	for _, ref := range unparseable {
		add(RuleFieldUnparseable, fmt.Sprintf("the wording backticks `%s`, and its shape could not be checked against the declared state", ref))
	}
	switch q.Kind {
	case KindNoul:
	case KindChoice:
		switch {
		case len(q.Options) > caps.MaxChoiceOptions:
			add(RuleTooManyOptions, fmt.Sprintf("the choice offers %d options and the route accepts %d", len(q.Options), caps.MaxChoiceOptions))
		case len(q.Options) < 2:
			add(RuleTooFewOptions, fmt.Sprintf("the choice offers %d options", len(q.Options)))
		}
		if q.Escape == "" {
			add(RuleNoEscape, "the choice declares no escape option, and a choice with no way out answers wrongly at confidence 1.00")
			break
		}
		named := false
		for _, o := range q.Options {
			if o.Name == q.Escape {
				named = true
			}
		}
		if !named {
			add(RuleUnknownEscape, fmt.Sprintf("the escape option %q is not one of the options", q.Escape))
		}
	case KindScore:
		switch {
		case len(q.Levels) > caps.MaxScoreLevels:
			add(RuleTooManyLevels, fmt.Sprintf("the score declares %d levels and the route accepts %d", len(q.Levels), caps.MaxScoreLevels))
		case len(q.Levels) < 2:
			add(RuleTooFewLevels, fmt.Sprintf("the score declares %d levels", len(q.Levels)))
		}
	}
	return out
}
