package tools

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"

	"tofu/internal/llm"
	"tofu/internal/turn"
)

type Typecheck struct {
	root     turn.Root
	checkers *turn.Typecheckers
}

func NewTypecheck(dir string, checkers *turn.Typecheckers) (Typecheck, error) {
	root, err := turn.NewRoot(dir)
	return Typecheck{root: root, checkers: checkers}, err
}

func (Typecheck) Name() string { return "typecheck" }

func (Typecheck) Definition() llm.Tool {
	return llm.Tool{
		Name: "typecheck",
		Description: "typechecks a typescript project with the tsc that keeps watching it, and returns the error count and the errors. " +
			"path is a file or a directory, the working directory when left out: the nearest tsconfig.json at or above it names the project, " +
			"and the errors shown are the ones under path, with the rest counted as other files. " +
			"a watching tsc rechecks only what changed, including changes made through the shell, so this answers in seconds where tsc --noEmit, " +
			"npm run typecheck or a build run through the shell take minutes on a large project: use this to check your work instead of them. " +
			"a project still on its first check says so, and the next call answers from the same tsc",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"path": map[string]any{"type": "string"}},
		},
	}
}

func (t Typecheck) Run(ctx context.Context, raw json.RawMessage) (turn.Result, error) {
	var args struct {
		Path string `json:"path"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return turn.Result{}, fmt.Errorf("typecheck: arguments are not the expected shape: %w", err)
		}
	}
	path := cmp.Or(args.Path, ".")
	resolved, err := t.root.Resolve(path)
	if err != nil {
		return turn.Result{}, fmt.Errorf("typecheck: %w", err)
	}
	report, err := t.checkers.Typecheck(ctx, resolved)
	if err != nil {
		return turn.Result{}, fmt.Errorf("typecheck: %w", err)
	}
	return turn.Result{Content: report, Command: path}, nil
}
