package judge

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"tofu/apps/cli/wiring"
	"tofu/internal/judge/questions"
	"tofu/internal/models/classifiers/core"
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

func resolveQuestions(input judgeInput) (wiring.Battery, error) {
	switch {
	case input.Library != "" && len(input.Questions) > 0:
		return wiring.Battery{}, errors.New("the request names both a library battery and inline questions")
	case input.Library != "":
		return wiring.ResolveLibrary(input.Library, "")
	case len(input.Questions) > 0:
		return resolveInline(input.Questions)
	default:
		return wiring.Battery{}, errors.New("the request names neither a library battery nor questions")
	}
}

func resolveInline(qs map[string]inlineQuestion) (wiring.Battery, error) {
	names := make([]string, 0, len(qs))
	for name := range qs {
		names = append(names, name)
	}
	sort.Strings(names)

	out := wiring.Battery{SetName: "inline", QuestionsVersion: 1, Kinds: make(map[string]questions.Kind, len(names))}
	for _, name := range names {
		jq, kind, err := toInlineJevQuestion(name, qs[name])
		if err != nil {
			return wiring.Battery{}, err
		}
		out.Questions = append(out.Questions, jq)
		out.Kinds[name] = kind
	}
	return out, nil
}

func toInlineJevQuestion(name string, in inlineQuestion) (core.Question, questions.Kind, error) {
	jq := core.Question{ID: name, Instructions: in.Instructions}
	switch in.Type {
	case string(questions.KindNoul):
		jq.Kind = core.QuestionNoul
		var criteria struct {
			True  string `json:"true"`
			False string `json:"false"`
		}
		if len(in.Criteria) > 0 {
			if err := json.Unmarshal(in.Criteria, &criteria); err != nil {
				return core.Question{}, "", fmt.Errorf("question %q: %w", name, err)
			}
		}
		jq.True, jq.False = criteria.True, criteria.False
		return jq, questions.KindNoul, nil
	case string(questions.KindChoice):
		jq.Kind = core.QuestionChoice
		for _, option := range in.Options {
			jq.Options = append(jq.Options, core.Option{Name: option.Name, Criteria: option.Criteria})
		}
		return jq, questions.KindChoice, nil
	case string(questions.KindScore):
		jq.Kind = core.QuestionScore
		var levels []string
		if len(in.Criteria) > 0 {
			if err := json.Unmarshal(in.Criteria, &levels); err != nil {
				return core.Question{}, "", fmt.Errorf("question %q: %w", name, err)
			}
		}
		jq.Levels = levels
		return jq, questions.KindScore, nil
	}
	return core.Question{}, "", fmt.Errorf("question %q has the unknown type %q", name, in.Type)
}
