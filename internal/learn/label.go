package learn

import (
	"embed"
	"errors"
	"slices"

	"tofu/internal/judge/question"
)

//go:embed learn_window@1.yaml
var questionFiles embed.FS

const questionSet = "learn_window"

func Questions() (question.Set, error) {
	sets, errs := question.LoadAll(questionFiles, ".")
	if err := errors.Join(errs...); err != nil {
		return question.Set{}, err
	}
	at := slices.IndexFunc(sets, func(s question.Set) bool { return s.Name == questionSet })
	if at < 0 {
		return question.Set{}, errors.New("learn: no " + questionSet + " question set")
	}
	return sets[at], nil
}

type WindowState struct {
	Message string `json:"message"`
	Before  string `json:"before"`
	After   string `json:"after"`
	Earlier string `json:"earlier"`
}

func (r Run) Windows() []WindowState {
	states := make([]WindowState, len(r.Said))
	for i, s := range r.Said {
		states[i] = WindowState{Message: s.Text, Before: s.Before, After: s.After}
	}
	for _, theme := range slices.Concat(r.Corrections, r.Watching, r.Seen) {
		first := theme.Quotes[0]
		for _, q := range theme.Quotes[1:] {
			at := slices.IndexFunc(r.Said, func(s Said) bool { return s.At.Equal(q.At) && s.Session == q.Session })
			if at >= 0 && q.Session != first.Session && states[at].Earlier == "" {
				states[at].Earlier = first.Text
			}
		}
	}
	return states
}

type Label struct {
	Said    int                `json:"said"`
	Row     string             `json:"row,omitempty"`
	Build   string             `json:"build,omitempty"`
	Cost    float64            `json:"cost"`
	Answers map[string]float64 `json:"answers,omitempty"`
	Chosen  map[string]string  `json:"chosen,omitempty"`
	Failure string             `json:"failure,omitempty"`
}

type Sent struct {
	Windows int     `json:"windows"`
	Bytes   int     `json:"bytes"`
	Cost    float64 `json:"cost"`
	Failed  int     `json:"failed"`
	Local   bool    `json:"local"`
	Written int     `json:"statements_written"`
	Kept    string  `json:"template_kept_because,omitempty"`
}
