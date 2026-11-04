package registry

import (
	"context"
	"encoding/json"
	"slices"

	"tofu/internal/models/llm/core"
	"tofu/internal/tools/code/search/engine"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/shell/process"
)

type cited struct {
	root contract.Root
	tool contract.Tool
}

func Checked(dir string, list []contract.Tool) ([]contract.Tool, error) {
	root, err := contract.NewRoot(dir)
	if err != nil {
		return nil, err
	}
	checked := make([]contract.Tool, len(list))
	for i, tool := range list {
		checked[i] = cited{root: root, tool: tool}
	}
	return checked, nil
}

func (c cited) Name() string { return c.tool.Name() }

func (c cited) Definition() core.Tool { return c.tool.Definition() }

func (c cited) Uncached() contract.Tool { return c.tool }

func (c cited) Run(ctx context.Context, raw json.RawMessage) (contract.Result, error) {
	result, err := c.tool.Run(ctx, raw)
	if err != nil {
		return result, err
	}
	var args struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(raw, &args)
	root := string(c.root)
	result.Content = withNote(result.Content, engine.Cite(root, result.Content, leadingDirs(root, args.Command)...).Text)
	return result, nil
}

func leadingDirs(root, command string) []string {
	var dirs []string
	for _, step := range process.Steps(root, command) {
		if !step.Moved {
			break
		}
		if step.Dir != root && !slices.Contains(dirs, step.Dir) {
			dirs = slices.Insert(dirs, 0, step.Dir)
		}
	}
	return dirs
}
