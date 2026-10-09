package state

import (
	"cmp"
	"encoding/json"
)

type Subject struct {
	Tool    string `json:"tool,omitempty"`
	Command string `json:"command,omitempty"`
	Path    string `json:"path,omitempty"`
	URL     string `json:"url,omitempty"`
}

func SubjectOf(state json.RawMessage) (Subject, error) {
	var recorded struct {
		Tool    string         `json:"tool"`
		Command string         `json:"command"`
		Input   map[string]any `json:"input"`
	}
	if err := json.Unmarshal(state, &recorded); err != nil {
		return Subject{}, err
	}
	text := func(field string) string {
		value, _ := recorded.Input[field].(string)
		return value
	}
	return Subject{Tool: recorded.Tool, Command: cmp.Or(text("command"), recorded.Command), Path: text("path"), URL: text("url")}, nil
}
