package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	catalogpolicy "tofu/catalog/policy"
	"tofu/catalog/questions"
	"tofu/internal/judge/jev"
	"tofu/internal/judge/policy"
	"tofu/internal/judge/question"
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
	Policy           *policy.Policy
	Mode             policy.Mode
	ModeReason       string
}

func resolveQuestions(input judgeInput) (battery, error) {
	switch {
	case input.Catalog != "" && len(input.Questions) > 0:
		return battery{}, errors.New("the request names both a catalog battery and inline questions")
	case input.Catalog != "":
		return resolveCatalog(input.Catalog)
	case len(input.Questions) > 0:
		return resolveInline(input.Questions)
	default:
		return battery{}, errors.New("the request names neither a catalog battery nor questions")
	}
}

func resolveCatalog(ref string) (battery, error) {
	layers, err := question.DefaultLayers(questions.Files())
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

func resolvePolicy(ref string, set battery) (policy.Policy, error) {
	if !strings.ContainsRune(ref, '@') {
		return policy.Policy{}, fmt.Errorf("the policy %q names no version; a policy is named name@version, so it never resolves silently to the wrong one", ref)
	}
	pol, _, err := loadPolicyPoint(ref)
	if err != nil {
		return policy.Policy{}, err
	}
	if pol.Questions != set.SetName || pol.QuestionsVersion != set.QuestionsVersion {
		return policy.Policy{}, fmt.Errorf("policy %s names the question set %s@%d and the battery resolved %s@%d",
			ref, pol.Questions, pol.QuestionsVersion, set.SetName, set.QuestionsVersion)
	}
	return pol, nil
}

func loadPolicyPoint(ref string) (policy.Policy, policy.Origin, error) {
	layers, err := question.DefaultLayers(questions.Files())
	if err != nil {
		return policy.Policy{}, "", err
	}
	set, _, err := question.Resolve(ref, layers)
	if err != nil {
		return policy.Policy{}, "", err
	}
	return policy.LoadPoint(catalogpolicy.Files(), ref, set)
}
