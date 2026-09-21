package tools

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"tofu/internal/llm"
	"tofu/internal/search"
	"tofu/internal/turn"
)

type Symbols struct {
	root turn.Root
}

func NewSymbols(dir string) (Symbols, error) {
	root, err := turn.NewRoot(dir)
	return Symbols{root: root}, err
}

func (s Symbols) Name() string { return "symbols" }

func (s Symbols) Definition() llm.Tool {
	return llm.Tool{
		Name: "symbols",
		Description: "answers where one go identifier is declared, what that declaration calls, and every place that calls it, from the go parser rather than from a text match. " +
			"reach for it before changing a function, because it says what the change breaks: search says the name appears in these declarations and this says the name is declared here and called from these three places. " +
			"it reads only go files, it matches on the identifier rather than resolving types, " +
			"so two symbols sharing one name come back as one answer and a call made through an interface or a function value is not found, and the result says so. " +
			ignoredWalkDescription,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":            map[string]any{"type": "string", "description": "the identifier to look up, exactly as it is spelled in the source, for instance Resolve"},
				"path":            map[string]any{"type": "string"},
				"include_ignored": map[string]any{"type": "boolean"},
			},
			"required": []string{"name"},
		},
	}
}

type symbolsArgs struct {
	Name           string `json:"name"`
	Path           string `json:"path"`
	IncludeIgnored bool   `json:"include_ignored"`
}

func (s Symbols) Run(_ context.Context, raw json.RawMessage) (turn.Result, error) {
	var args symbolsArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return turn.Result{}, fmt.Errorf("symbols: arguments are not the expected shape: %w", err)
	}
	if strings.TrimSpace(args.Name) == "" {
		return turn.Result{}, errors.New("symbols: name is required, and it is one identifier rather than a pattern")
	}
	under := cmp.Or(args.Path, ".")
	listed, err := filesUnder(s.root, under, args.IncludeIgnored)
	if err != nil {
		return turn.Result{}, fmt.Errorf("symbols: %w", err)
	}
	under = listed.under
	graph, err := search.Symbols(string(s.root), listed.files, args.Name)
	if err != nil {
		return turn.Result{}, err
	}
	return turn.Result{
		Content: withNote("symbols "+args.Name+" under "+under+"\n"+graph.Text, listed.note),
		Command: "symbols " + args.Name + " under " + under,
	}, nil
}
