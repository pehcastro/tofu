package tools

import (
	"context"
	"encoding/json"
	"slices"

	"tofu/internal/llm"
	"tofu/internal/search"
	"tofu/internal/shell"
	"tofu/internal/turn"
)

type cited struct {
	root turn.Root
	tool turn.Tool
}

func Checked(dir string, list []turn.Tool) ([]turn.Tool, error) {
	root, err := turn.NewRoot(dir)
	if err != nil {
		return nil, err
	}
	checked := make([]turn.Tool, len(list))
	for i, tool := range list {
		checked[i] = cited{root: root, tool: tool}
	}
	return checked, nil
}

func (c cited) Name() string { return c.tool.Name() }

func (c cited) Definition() llm.Tool { return c.tool.Definition() }

func (c cited) Uncached() turn.Tool { return c.tool }

func (c cited) Run(ctx context.Context, raw json.RawMessage) (turn.Result, error) {
	result, err := c.tool.Run(ctx, raw)
	if err != nil {
		return result, err
	}
	var args struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(raw, &args)
	root := string(c.root)
	result.Content = withNote(result.Content, search.Cite(root, result.Content, leadingDirs(root, args.Command)...).Text)
	return result, nil
}

func leadingDirs(root, command string) []string {
	var dirs []string
	for _, step := range shell.Steps(root, command) {
		if !step.Moved {
			break
		}
		if step.Dir != root && !slices.Contains(dirs, step.Dir) {
			dirs = slices.Insert(dirs, 0, step.Dir)
		}
	}
	return dirs
}
