package tools

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"tofu/internal/llm"
	"tofu/internal/search"
	"tofu/internal/turn"
)

type Grep struct {
	root turn.Root
}

func NewGrep(dir string) (Grep, error) {
	root, err := turn.NewRoot(dir)
	return Grep{root: root}, err
}

func (g Grep) Name() string { return "grep" }

func (g Grep) Definition() llm.Tool {
	return llm.Tool{
		Name: "grep",
		Description: "searches the text of every file under the turn's working directory for a regular expression " +
			"and returns every matching line as path:line:text. " +
			"the expression is go regexp syntax, which is the same as perl for everything short of backreferences. " +
			ignoredWalkDescription + ", and it never reads a file that holds a null byte. " +
			"zero matches is an answer, not an error: the result says how many files were searched. " +
			"it does not list file names by pattern, which glob does, and it does not change anything",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern":         map[string]any{"type": "string"},
				"path":            map[string]any{"type": "string"},
				"glob":            map[string]any{"type": "string"},
				"include_ignored": map[string]any{"type": "boolean"},
			},
			"required": []string{"pattern"},
		},
	}
}

type grepArgs struct {
	Pattern        string `json:"pattern"`
	Path           string `json:"path"`
	Glob           string `json:"glob"`
	IncludeIgnored bool   `json:"include_ignored"`
}

func (g Grep) Run(_ context.Context, raw json.RawMessage) (turn.Result, error) {
	var args grepArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return turn.Result{}, fmt.Errorf("grep: arguments are not the expected shape: %w", err)
	}
	if strings.TrimSpace(args.Pattern) == "" {
		return turn.Result{}, errors.New("grep: pattern is required")
	}
	expression, err := regexp.Compile(args.Pattern)
	if err != nil {
		return turn.Result{}, fmt.Errorf("grep: %q is not a regular expression: %w", args.Pattern, err)
	}
	under := cmp.Or(args.Path, ".")
	listed, err := filesUnder(g.root, under, args.IncludeIgnored)
	if err != nil {
		return turn.Result{}, fmt.Errorf("grep: %w", err)
	}
	under = listed.under

	var lines []string
	searched, matched, binary := 0, 0, 0
	for _, rel := range listed.files {
		if args.Glob != "" && !matchesPattern(args.Glob, rel) {
			continue
		}
		body, err := os.ReadFile(filepath.Join(string(g.root), filepath.FromSlash(rel)))
		if err != nil {
			return turn.Result{}, fmt.Errorf("grep: %w", err)
		}
		if bytes.IndexByte(body, 0) >= 0 {
			binary++
			continue
		}
		searched++
		hits := 0
		for number, line := range strings.Split(string(body), "\n") {
			line = strings.TrimSuffix(line, "\r")
			if expression.MatchString(line) {
				hits++
				lines = append(lines, rel+":"+strconv.Itoa(number+1)+":"+line)
			}
		}
		if hits > 0 {
			matched++
		}
	}

	note := listed.note
	if binary > 0 {
		note = strings.TrimSpace(note + " " + search.Note(search.BinarySkipped,
			fmt.Sprintf("%d of the %d files under %s hold a null byte and were not searched", binary, binary+searched, under)))
	}
	command := "grep " + args.Pattern + " under " + under
	if len(lines) == 0 {
		return turn.Result{
			Content: withNote(fmt.Sprintf("%q matches no line in any of the %d text files under %s. the files were read: this is an answer, not a failure",
				args.Pattern, searched, under), note),
			Command: command,
		}, nil
	}
	return turn.Result{
		Content: withNote(fmt.Sprintf("%d matching lines in %d of the %d text files under %s\n%s\n",
			len(lines), matched, searched, under, strings.Join(lines, "\n")), note),
		Command: command,
	}, nil
}
