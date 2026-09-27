package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"tofu/internal/llm"
	"tofu/internal/settings"
	"tofu/internal/turn"
)

type Settings struct {
	global, project string
}

func NewSettings(global, project string) Settings {
	return Settings{global: global, project: project}
}

func (Settings) Name() string { return "settings" }

func (Settings) Definition() llm.Tool {
	return llm.Tool{
		Name: "settings",
		Description: "asks the person to change one of their settings, and only " + settings.SubAgentsPerTurn + " or " + settings.SubAgentDepth +
			". the person answers yes or no before anything is written; on yes the global settings file changes and the next spawn in this turn reads the new value",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"key":   map[string]any{"type": "string", "enum": []string{settings.SubAgentsPerTurn, settings.SubAgentDepth}},
				"value": map[string]any{"type": "integer", "minimum": 1},
			},
			"required": []string{"key", "value"},
		},
	}
}

func (s Settings) Run(_ context.Context, raw json.RawMessage) (turn.Result, error) {
	var args struct {
		Key   string `json:"key"`
		Value int    `json:"value"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return turn.Result{}, fmt.Errorf("settings: arguments are not the expected shape: %w", err)
	}
	if args.Key != settings.SubAgentsPerTurn && args.Key != settings.SubAgentDepth {
		return turn.Result{}, fmt.Errorf("settings: %q is not a setting this tool changes; it changes %s and %s only", args.Key, settings.SubAgentsPerTurn, settings.SubAgentDepth)
	}
	store, err := settings.Open(s.global, s.project)
	if err != nil {
		return turn.Result{}, fmt.Errorf("settings: %w", err)
	}
	if err := store.Set(settings.Global, args.Key, args.Value); err != nil {
		return turn.Result{}, fmt.Errorf("settings: %w", err)
	}
	return turn.Result{Content: fmt.Sprintf("%s set to %d in %s; the next spawn reads %d", args.Key, args.Value, s.global, store.Int(args.Key))}, nil
}
