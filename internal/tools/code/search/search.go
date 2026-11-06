package search

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"tofu/internal/konst"
	"tofu/internal/models/llm/core"
	repomap "tofu/internal/tools/code/map"
	"tofu/internal/tools/code/search/engine"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/files/glob"
)

type Search struct {
	root   contract.Root
	ranker repomap.Map
}

func NewSearch(dir string) (Search, error) {
	root, err := contract.NewRoot(dir)
	ranker, mapErr := repomap.New(dir)
	return Search{root: root, ranker: ranker}, cmp.Or(err, mapErr)
}

func (s Search) Name() string { return "search" }

func (s Search) Definition() core.Tool {
	return core.Tool{
		Name: "search",
		Description: "the only content search: matches a go regexp against every text file under the working directory. " +
			"a match in .go, .ts, .tsx, .js, .jsx, .py or .rs returns its whole declaration, marked code, comment or string; other files return the lines around it. " +
			fmt.Sprintf("matches in more than %d files return each declaration's name and matching lines instead, %d files a page, and skip passes over that many files to reach the next page. ", engine.FilesPerPage, engine.FilesPerPage) +
			"files are ordered by the map tool's rank, which task and files seed exactly as they seed map; task defaults to the turn's own task, so pass it only to rank for different work, and files for the files it touches. " +
			"spends max_tokens, not lines, and says how many matched and how many matching lines the page cut. " +
			"skips what .gitignore skips unless include_ignored. " +
			"when it returns less than you need, narrow path or raise max_tokens",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern":         map[string]any{"type": "string"},
				"path":            map[string]any{"type": "string"},
				"max_tokens":      map[string]any{"type": "integer"},
				"skip":            map[string]any{"type": "integer"},
				"include_ignored": map[string]any{"type": "boolean"},
				"task":            map[string]any{"type": "string"},
				"files":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			},
			"required": []string{"pattern"},
		},
	}
}

type searchArgs struct {
	Pattern        string   `json:"pattern"`
	Path           string   `json:"path"`
	MaxTokens      int      `json:"max_tokens"`
	Skip           int      `json:"skip"`
	IncludeIgnored bool     `json:"include_ignored"`
	Task           string   `json:"task"`
	Files          []string `json:"files"`
}

func (s Search) Run(ctx context.Context, raw json.RawMessage) (contract.Result, error) {
	var args searchArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return contract.Result{}, fmt.Errorf("search: arguments are not the expected shape: %w", err)
	}
	if strings.TrimSpace(args.Pattern) == "" {
		return contract.Result{}, errors.New("search: pattern is required")
	}
	if args.Skip < 0 {
		return contract.Result{}, fmt.Errorf("search: skip is a count of files to pass over and cannot be %d", args.Skip)
	}
	expression, err := regexp.Compile(args.Pattern)
	if err != nil {
		return contract.Result{}, fmt.Errorf("search: %q is not a regular expression: %w", args.Pattern, err)
	}
	under := cmp.Or(args.Path, ".")
	listed, err := glob.FilesUnder(s.root, under, args.IncludeIgnored)
	if err != nil {
		return contract.Result{}, fmt.Errorf("search: %w", err)
	}
	under = listed.Under
	result, err := engine.Find(engine.Request{
		Root:      string(s.root),
		Files:     listed.Files,
		Sizes:     listed.Sizes,
		Pattern:   expression,
		MaxTokens: engine.Unbudgeted,
	})
	if err != nil {
		return contract.Result{}, err
	}
	ranking := s.rankByMap(ctx, args, listed.Files, result.Units)
	budget := args.MaxTokens
	if budget <= 0 {
		budget = konst.SearchTokenBudget
	}
	page, err := result.Page(args.Skip, budget)
	if err != nil {
		return contract.Result{}, err
	}
	hits := 0
	for _, unit := range result.Units {
		hits += len(unit.Matches)
	}
	return contract.Result{
		Content: glob.WithNote(fmt.Sprintf("search %s under %s\n%s%s", args.Pattern, under, ranking, page), listed.Note),
		Command: args.Pattern + " under " + under,
		Hits:    &hits,
	}, nil
}

type turnTaskKey struct{}

func WithTurnTask(ctx context.Context, task string) context.Context {
	return context.WithValue(ctx, turnTaskKey{}, task)
}

func (s Search) rankByMap(ctx context.Context, args searchArgs, listed []string, units []engine.Unit) string {
	files := map[string]bool{}
	for _, unit := range units {
		files[unit.Path] = true
	}
	if len(files) < 2 {
		return ""
	}
	over := ""
	if turned, _ := ctx.Value(turnTaskKey{}).(string); args.Task == "" && turned != "" {
		args.Task, over = turned, " over the turn's task"
	}
	ranking, err := s.ranker.Rank(listed, args.Task, args.Files)
	if err != nil {
		return "unranked, in score then path order: map: " + err.Error() + "\n"
	}
	slices.SortStableFunc(units, func(a, b engine.Unit) int { return cmp.Compare(ranking.ByPath[b.Path], ranking.ByPath[a.Path]) })
	return "ranked by the map" + over + ", seeded by " + ranking.Seeded + "\n"
}
