package tools

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"tofu/internal/llm"
	"tofu/internal/search"
	"tofu/internal/turn"
)

type Search struct {
	root turn.Root
}

func NewSearch(dir string) (Search, error) {
	root, err := turn.NewRoot(dir)
	return Search{root: root}, err
}

func (s Search) Name() string { return "search" }

func (s Search) Definition() llm.Tool {
	return llm.Tool{
		Name: "search",
		Description: "finds a regular expression under the turn's working directory and returns the whole declaration each match sits inside, " +
			"so a match in a go file comes back as the entire function, method, type or constant rather than the line. " +
			"it says whether each match is in code, in a comment or in a string literal, which the parser knows and a line does not. " +
			"a file it cannot parse, which is every file that is not go and any go file with a syntax error, " +
			"comes back as the lines around the match and is counted as a fallback. " +
			"it spends a token budget rather than a line count: over the budget it returns fewer whole units and never a cut one. " +
			ignoredWalkDescription + ". " +
			"use grep when you want every matching line of a text file, and read when you already know which file and which lines you want",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern":         map[string]any{"type": "string"},
				"path":            map[string]any{"type": "string"},
				"max_tokens":      map[string]any{"type": "integer"},
				"include_ignored": map[string]any{"type": "boolean"},
			},
			"required": []string{"pattern"},
		},
	}
}

type searchArgs struct {
	Pattern        string `json:"pattern"`
	Path           string `json:"path"`
	MaxTokens      int    `json:"max_tokens"`
	IncludeIgnored bool   `json:"include_ignored"`
}

func (s Search) Run(_ context.Context, raw json.RawMessage) (turn.Result, error) {
	var args searchArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return turn.Result{}, fmt.Errorf("search: arguments are not the expected shape: %w", err)
	}
	if strings.TrimSpace(args.Pattern) == "" {
		return turn.Result{}, errors.New("search: pattern is required")
	}
	expression, err := regexp.Compile(args.Pattern)
	if err != nil {
		return turn.Result{}, fmt.Errorf("search: %q is not a regular expression: %w", args.Pattern, err)
	}
	under := cmp.Or(args.Path, ".")
	listed, err := filesUnder(s.root, under, args.IncludeIgnored)
	if err != nil {
		return turn.Result{}, fmt.Errorf("search: %w", err)
	}
	result, err := search.Find(search.Request{
		Root:      string(s.root),
		Files:     listed.files,
		Pattern:   expression,
		MaxTokens: args.MaxTokens,
	})
	if err != nil {
		return turn.Result{}, err
	}
	return turn.Result{
		Content: withNote(fmt.Sprintf("search %s under %s\n%s", args.Pattern, under, result.Text), listed.note),
		Command: "search " + args.Pattern + " under " + under,
	}, nil
}
