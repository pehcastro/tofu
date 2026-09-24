package thrift

import (
	"fmt"

	"tofu/internal/judge/thrift/unit"
)

const NeededQuestion = unit.NeededQuestion

type Mark = unit.Mark

type State struct {
	Task      string `json:"task"`
	Tool      string `json:"tool"`
	Command   string `json:"command"`
	Paragraph string `json:"paragraph"`
	Position  string `json:"position"`
}

func BuildState(paragraph string, index, total int, tool, command, task string) State {
	return State{
		Task:      task,
		Tool:      tool,
		Command:   command,
		Paragraph: paragraph,
		Position:  unit.Position(index, total),
	}
}

func Decide(index int, score float64, answered bool, keepAt float64) (Mark, error) {
	if !answered {
		return Mark{}, fmt.Errorf("thrift: unit %d carries no %q answer, so it cannot be judged", index, NeededQuestion)
	}
	if score >= keepAt {
		return Mark{Keep: true, Reason: fmt.Sprintf("still needed %.2f", score)}, nil
	}
	return Mark{Reason: fmt.Sprintf("still needed %.2f, under %.2f", score, keepAt)}, nil
}
