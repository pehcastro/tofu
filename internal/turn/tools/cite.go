package tools

import (
	"context"
	"encoding/json"

	"tofu/internal/llm"
	"tofu/internal/search"
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

func (c cited) Run(ctx context.Context, raw json.RawMessage) (turn.Result, error) {
	result, err := c.tool.Run(ctx, raw)
	if err != nil {
		return result, err
	}
	result.Content = withNote(result.Content, search.Cite(string(c.root), result.Content).Text)
	return result, nil
}
