package sift

import (
	"fmt"
	"regexp"
	"strings"
)

var errorShaped = regexp.MustCompile(`(?i)\b(errors?|failed|failures?|fatal|panic|traceback|exception|denied|no such file|not found|undefined|unresolved|exited [0-9]+)\b`)

func ShellCheap(unit Unit) Mark {
	if unit.Held != NotHeld {
		return Mark{Keep: true, Reason: string(unit.Held)}
	}
	if errorShaped.MatchString(unit.Text) {
		return Mark{Keep: true, Reason: "an error-shaped line"}
	}
	return Mark{Reason: "neither an end of the output nor error-shaped"}
}

type ShellState struct {
	Task     string `json:"task"`
	Command  string `json:"command"`
	ExitCode int    `json:"exit_code"`
	Stream   Stream `json:"stream"`
	Position string `json:"position"`
	Chunk    string `json:"chunk"`
}

func BuildShellState(result Shell, units []Unit, index int, task string) ShellState {
	unit := units[index]
	return ShellState{
		Task:     task,
		Command:  result.Command,
		ExitCode: result.ExitCode,
		Stream:   unit.Stream,
		Position: fmt.Sprintf("chunk %d of %d", index+1, len(units)),
		Chunk:    strings.TrimRight(unit.Text, "\n"),
	}
}

const NeededQuestion = "still_needed"

func DecideShell(unit Unit, answers map[string]float64, keepAt float64) (Mark, error) {
	if unit.Held != NotHeld {
		return Mark{Keep: true, Reason: string(unit.Held)}, nil
	}
	needed, ok := answers[NeededQuestion]
	if !ok {
		return Mark{}, fmt.Errorf("sift: the answer carries no %q, so chunk %d cannot be judged", NeededQuestion, unit.Index)
	}
	if needed >= keepAt {
		return Mark{Keep: true, Reason: fmt.Sprintf("still needed %.2f", needed)}, nil
	}
	return Mark{Reason: fmt.Sprintf("still needed %.2f, under %.2f", needed, keepAt)}, nil
}
