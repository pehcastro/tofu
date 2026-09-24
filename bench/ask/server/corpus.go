package server

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"

	"tofu/bench/corpus"
)

const (
	Session1Dir = `F:\localhost\admin-template\.tofu\sessions\turn-18d7f94ce7a62138`
	Session2Dir = `F:\localhost\admin-template\.tofu\sessions\turn-18d81f3d00193028`
)

type Moment struct {
	TurnID     string
	Task       string
	Scripts    []Script
	StepsTotal int
	GuessStep  int
	Ideal      string
}

func (m Moment) StepsRemoved() int {
	return m.StepsTotal - m.GuessStep + 1
}

var guessPattern = regexp.MustCompile(`\bnpm run\b|\bnpm start\b`)

func Load(dir, ideal string) (Moment, error) {
	turn, err := corpus.ReadTurnDir(dir)
	if err != nil {
		return Moment{}, err
	}
	scripts, err := declaredScripts(turn)
	if err != nil {
		return Moment{}, err
	}
	guess := 0
	for _, s := range turn.Steps {
		for _, tc := range s.ToolCalls {
			if guessPattern.MatchString(tc.Command) {
				guess = s.Index
			}
		}
		if guess != 0 {
			break
		}
	}
	if guess == 0 {
		return Moment{}, fmt.Errorf("bench/ask/server: %s never runs a start-server guess", dir)
	}
	return Moment{
		TurnID:     turn.ID,
		Task:       turn.Task,
		Scripts:    scripts,
		StepsTotal: len(turn.Steps),
		GuessStep:  guess,
		Ideal:      ideal,
	}, nil
}

type packageJSON struct {
	Scripts map[string]string `json:"scripts"`
}

func declaredScripts(turn corpus.RecordedTurn) ([]Script, error) {
	for _, m := range turn.Messages {
		var pkg packageJSON
		if json.Unmarshal([]byte(m.Content), &pkg) == nil && len(pkg.Scripts) > 0 {
			return sortedScripts(pkg.Scripts), nil
		}
	}
	return nil, fmt.Errorf("bench/ask/server: no package.json scripts found in the recorded turn")
}

func sortedScripts(raw map[string]string) []Script {
	names := make([]string, 0, len(raw))
	for name := range raw {
		names = append(names, name)
	}
	sort.Strings(names)
	scripts := make([]Script, 0, len(names))
	for _, name := range names {
		scripts = append(scripts, Script{Name: name, Command: raw[name]})
	}
	return scripts
}
