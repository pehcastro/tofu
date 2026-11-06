package typecheck

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"

	"tofu/internal/models/llm/core"
	"tofu/internal/tools/code/typecheck/checkers"
	"tofu/internal/tools/contract"
)

type Typecheck struct {
	root     contract.Root
	checkers *checkers.Typecheckers
}

func NewTypecheck(dir string, checkers *checkers.Typecheckers) (Typecheck, error) {
	root, err := contract.NewRoot(dir)
	return Typecheck{root: root, checkers: checkers}, err
}

func (Typecheck) Name() string { return "typecheck" }

func (Typecheck) Definition() core.Tool {
	return core.Tool{
		Name: "typecheck",
		Description: "typechecks the code under path with the project's own checker and returns the error count and the errors as path:line. " +
			"the checker is chosen by the files under path: go build then go vet for Go (the nearest go.mod), cargo check for a Rust crate and rustc for a .rs file outside one, " +
			"pyright or else mypy for Python, and for TypeScript tsc, vue-tsc or svelte-check as package.json declares, kept watching. " +
			"path is a file or a directory, the working directory when left out. for TypeScript the nearest tsconfig.json at or above it names the project, " +
			"the errors shown are the ones under path with the rest counted as other files, and the watching checker rechecks only what changed, including changes made through the shell, " +
			"so this answers in seconds where tsc --noEmit, npx svelte-check, npm run check or a build run through the shell take minutes on a large project: use this to check your work instead of them. " +
			"a project still on its first check says so, and the next call answers from the same checker. " +
			"explain takes the raw text of a compiler, checker or test failure instead and runs nothing: it returns each error with its code window and the declarations it names, " +
			"the same error at many places folded into one",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    map[string]any{"type": "string"},
				"explain": map[string]any{"type": "string"},
			},
		},
	}
}

func (t Typecheck) Run(ctx context.Context, raw json.RawMessage) (contract.Result, error) {
	var args struct {
		Path    string `json:"path"`
		Explain string `json:"explain"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return contract.Result{}, fmt.Errorf("typecheck: arguments are not the expected shape: %w", err)
		}
	}
	path := cmp.Or(args.Path, ".")
	resolved, err := t.root.Resolve(path)
	if err != nil {
		return contract.Result{}, fmt.Errorf("typecheck: %w", err)
	}
	if args.Explain != "" {
		result := checkers.Explain(args.Explain, resolved)
		result.Command = path
		return result, nil
	}
	result, err := t.checkers.Check(ctx, resolved)
	if err != nil {
		return contract.Result{}, fmt.Errorf("typecheck: %w", err)
	}
	result.Command = path
	return result, nil
}
