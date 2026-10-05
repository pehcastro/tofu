package tools

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"tofu/internal/konst"
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
		Description: "finds text anywhere under the turn's working directory and is the only tool that reads file contents to find something: there is no grep tool. " +
			"the pattern is go regexp syntax, which is the same as perl for everything short of backreferences, and it is matched against every line of every text file. " +
			"it returns the whole declaration each match sits inside, " +
			"so a match in a .go, .ts, .tsx, .js, .jsx, .py or .rs file comes back as the entire function, method, class or type rather than the line. " +
			"it says whether each match is in code, in a comment or in a string literal, which the parser knows and a line does not. " +
			"a file it cannot parse, which is every file in any other language and any of those files with unbalanced brackets or a syntax error, " +
			"comes back as the lines around the match and is counted as a fallback, " +
			"and a fallback line longer than " + strconv.Itoa(konst.SearchLineWidth) + " bytes, such as a minified file, is cut to that width around the match " +
			"with how many characters were cut on each side. " +
			"a file over " + strconv.Itoa(konst.SearchFileByteCap>>20) + " MB is not read, and the result counts it. " +
			"it spends a token budget rather than a line count: over the budget it returns fewer units and never cuts a declaration short, " +
			"and it says how many matched and how many came back so a short answer is never mistaken for the whole. " +
			ignoredWalkDescription + ". " +
			"when it returned less than you needed, narrow path or raise max_tokens rather than running a shell search, " +
			"which descends into every ignored directory and hands back every matching line instead of a bounded answer. " +
			"read is for when you already know which file and which lines you want",
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
