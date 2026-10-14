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
		Description: "the only content search: matches a go regexp against every text file under the working directory. " +
			"a match in .go, .ts, .tsx, .js, .jsx, .py or .rs returns its whole declaration, marked code, comment or string; other files return the lines around it. " +
			"spends max_tokens, not lines, and says how many matched. " +
			"skips what .gitignore skips unless include_ignored. " +
			"when it returns less than you need, narrow path or raise max_tokens",
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
	under = listed.under
	result, err := search.Find(search.Request{
		Root:      string(s.root),
		Files:     listed.files,
		Sizes:     listed.sizes,
		Pattern:   expression,
		MaxTokens: args.MaxTokens,
	})
	if err != nil {
		return turn.Result{}, err
	}
	return turn.Result{
		Content: withNote(fmt.Sprintf("search %s under %s\n%s", args.Pattern, under, result.Text), listed.note),
		Command: args.Pattern + " under " + under,
	}, nil
}
