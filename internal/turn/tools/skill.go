package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"tofu/internal/llm"
	"tofu/internal/skill"
	"tofu/internal/turn"
)

type Skill struct {
	skills []skill.Skill
}

func NewSkill(skills []skill.Skill) Skill { return Skill{skills: skills} }

func (s Skill) Name() string { return "skill" }

func (s Skill) Definition() llm.Tool {
	return llm.Tool{
		Name: "skill",
		Description: "loads a skill the system prompt lists, by name, and returns its SKILL.md without the front matter. " +
			"path reads another file inside that skill's folder, relative to it, when the skill's body names one",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string"},
				"path": map[string]any{"type": "string"},
			},
			"required": []string{"name"},
		},
	}
}

func (s Skill) Run(_ context.Context, raw json.RawMessage) (turn.Result, error) {
	var args struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return turn.Result{}, fmt.Errorf("skill: arguments are not the expected shape: %w", err)
	}
	body, err := skill.Load(s.skills, args.Name, args.Path)
	if err != nil {
		return turn.Result{}, fmt.Errorf("skill: %w", err)
	}
	command := args.Name
	if args.Path != "" {
		command += "/" + args.Path
	}
	return turn.Result{Content: body, Command: command}, nil
}
