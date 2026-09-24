package sift

import (
	"fmt"
	"strings"

	"tofu/internal/judge/thrift/unit"
)

const NeededQuestion = unit.NeededQuestion

type Mark = unit.Mark

type State struct {
	Paragraph   string `json:"paragraph"`
	Task        string `json:"task"`
	AlreadySaid string `json:"already_said"`
	Position    string `json:"position"`
}

func BuildState(parts []Part, index int, task string) State {
	var said strings.Builder
	for _, p := range parts[:index] {
		said.WriteString(strings.TrimSpace(p.Text))
		said.WriteString("\n")
	}
	return State{
		Paragraph:   parts[index].Text,
		Task:        task,
		AlreadySaid: said.String(),
		Position:    unit.Position(index, len(parts)),
	}
}

const answerQuestion = "answers_the_task"

type Rule struct {
	AnswerFloor    float64
	PaddingCeiling float64
}

func DefaultRule() Rule {
	return Rule{AnswerFloor: 1.5, PaddingCeiling: 2.0}
}

func Decide(scores map[string]float64, rule Rule) (Mark, error) {
	answer, err := score(scores, answerQuestion)
	if err != nil {
		return Mark{}, err
	}
	worstName, worst := "", 0.0
	for _, name := range []string{"preamble", "recap", "hedging", "jargon", "selling"} {
		value, err := score(scores, name)
		if err != nil {
			return Mark{}, err
		}
		if value >= worst {
			worstName, worst = name, value
		}
	}
	if answer >= rule.AnswerFloor || worst < rule.PaddingCeiling {
		return Mark{Keep: true}, nil
	}
	return Mark{Reason: fmt.Sprintf("%s %.2f, answer %.2f", worstName, worst, answer)}, nil
}

func score(scores map[string]float64, name string) (float64, error) {
	value, ok := scores[name]
	if !ok {
		return 0, fmt.Errorf("sift: the answer carries no %q, so the paragraph cannot be judged", name)
	}
	return value, nil
}
