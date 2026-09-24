package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"tofu/internal/judge/gate"
	"tofu/internal/judge/jev"
	"tofu/internal/judge/question"
	shipped "tofu/library"
	"tofu/library/questions"
)

type inlineOption struct {
	Name     string `json:"name"`
	Criteria any    `json:"criteria"`
}

type inlineQuestion struct {
	Type         string          `json:"type"`
	Instructions string          `json:"instructions"`
	Criteria     json.RawMessage `json:"criteria"`
	Options      []inlineOption  `json:"options"`
}

type battery struct {
	Questions        []jev.Question
	Kinds            map[string]question.Kind
	SetName          string
	QuestionsVersion int
	Rule             *gate.Rule
	Mode             gate.Mode
	ModeReason       string
}

func resolveQuestions(input judgeInput) (battery, error) {
	switch {
	case input.Library != "" && len(input.Questions) > 0:
		return battery{}, errors.New("the request names both a library battery and inline questions")
	case input.Library != "":
		return resolveLibrary(input.Library, "")
	case len(input.Questions) > 0:
		return resolveInline(input.Questions)
	default:
		return battery{}, errors.New("the request names neither a library battery nor questions")
	}
}

func resolveLibrary(ref, dir string) (battery, error) {
	layers, err := question.Layers(questions.Files(), dir)
	if err != nil {
		return battery{}, err
	}
	set, _, err := question.Resolve(ref, layers)
	if err != nil {
		return battery{}, err
	}
	jevQuestions := make([]jev.Question, 0, len(set.Questions))
	kinds := make(map[string]question.Kind, len(set.Questions))
	for _, q := range set.Questions {
		jevQuestions = append(jevQuestions, q.ToJev())
		kinds[q.Name] = q.Kind
	}
	return battery{Questions: jevQuestions, Kinds: kinds, SetName: set.Name, QuestionsVersion: set.QuestionsVersion}, nil
}

func resolveInline(qs map[string]inlineQuestion) (battery, error) {
	names := make([]string, 0, len(qs))
	for name := range qs {
		names = append(names, name)
	}
	sort.Strings(names)

	out := battery{SetName: "inline", QuestionsVersion: 1, Kinds: make(map[string]question.Kind, len(names))}
	for _, name := range names {
		jq, kind, err := toInlineJevQuestion(name, qs[name])
		if err != nil {
			return battery{}, err
		}
		out.Questions = append(out.Questions, jq)
		out.Kinds[name] = kind
	}
	return out, nil
}

func toInlineJevQuestion(name string, in inlineQuestion) (jev.Question, question.Kind, error) {
	jq := jev.Question{ID: name, Instructions: in.Instructions}
	switch in.Type {
	case string(question.KindNoul):
		jq.Kind = jev.QuestionNoul
		var criteria struct {
			True  string `json:"true"`
			False string `json:"false"`
		}
		if len(in.Criteria) > 0 {
			if err := json.Unmarshal(in.Criteria, &criteria); err != nil {
				return jev.Question{}, "", fmt.Errorf("question %q: %w", name, err)
			}
		}
		jq.True, jq.False = criteria.True, criteria.False
		return jq, question.KindNoul, nil
	case string(question.KindChoice):
		jq.Kind = jev.QuestionChoice
		for _, option := range in.Options {
			jq.Options = append(jq.Options, jev.Option{Name: option.Name, Criteria: option.Criteria})
		}
		return jq, question.KindChoice, nil
	case string(question.KindScore):
		jq.Kind = jev.QuestionScore
		var levels []string
		if len(in.Criteria) > 0 {
			if err := json.Unmarshal(in.Criteria, &levels); err != nil {
				return jev.Question{}, "", fmt.Errorf("question %q: %w", name, err)
			}
		}
		jq.Levels = levels
		return jq, question.KindScore, nil
	}
	return jev.Question{}, "", fmt.Errorf("question %q has the unknown type %q", name, in.Type)
}

func resolveRule(ref string, set battery) (gate.Rule, error) {
	if !strings.ContainsRune(ref, '@') {
		return gate.Rule{}, fmt.Errorf("the rule %q names no version; a rule is named name@version, so it never resolves silently to the wrong one", ref)
	}
	r, _, err := loadRulePoint(ref, "")
	if err != nil {
		return gate.Rule{}, err
	}
	if r.Questions != set.SetName || r.QuestionsVersion != set.QuestionsVersion {
		return gate.Rule{}, fmt.Errorf("rule %s names the question set %s@%d and the battery resolved %s@%d",
			ref, r.Questions, r.QuestionsVersion, set.SetName, set.QuestionsVersion)
	}
	return r, nil
}

func loadRulePoint(ref, dir string) (gate.Rule, gate.Origin, error) {
	layers, err := question.Layers(questions.Files(), dir)
	if err != nil {
		return gate.Rule{}, "", err
	}
	set, _, err := question.Resolve(ref, layers)
	if err != nil {
		return gate.Rule{}, "", err
	}
	return gate.LoadPoint(shipped.Files(), ref, set, dir)
}
