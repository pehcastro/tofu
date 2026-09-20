package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"boji/internal/llm"
	"boji/internal/search"
	"boji/internal/transform"
	"boji/internal/turn"
)

type Edit struct {
	root turn.Root
}

func NewEdit(dir string) (Edit, error) {
	root, err := turn.NewRoot(dir)
	return Edit{root: root}, err
}

func (e Edit) Name() string { return "edit" }

func (e Edit) Definition() llm.Tool {
	return llm.Tool{
		Name: "edit",
		Description: "replaces one exact stretch of text inside a file that already exists and leaves the rest of it untouched. " +
			"old_string is copied from the file character for character, including indentation and line breaks, " +
			"and it has to appear exactly once: when it appears more than once, add the lines above or below it until it is unique. " +
			"new_string is what it becomes, and an empty new_string deletes it. " +
			"the result is a unified diff of what changed. " +
			"use write instead to create a file or to replace the whole of one",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":       map[string]any{"type": "string"},
				"old_string": map[string]any{"type": "string"},
				"new_string": map[string]any{"type": "string"},
			},
			"required": []string{"path", "old_string", "new_string"},
		},
	}
}

type editArgs struct {
	Path string `json:"path"`
	Old  string `json:"old_string"`
	New  string `json:"new_string"`
}

func (e Edit) Run(_ context.Context, raw json.RawMessage) (turn.Result, error) {
	var args editArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return turn.Result{}, fmt.Errorf("edit: arguments are not the expected shape: %w", err)
	}
	if args.Old == "" {
		return turn.Result{}, errors.New("edit: old_string is the text being replaced and it cannot be empty: write creates a file")
	}
	if args.Old == args.New {
		return turn.Result{}, errors.New("edit: old_string and new_string are the same text, so there is nothing to change")
	}
	resolved, err := e.root.Resolve(args.Path)
	if err != nil {
		return turn.Result{}, fmt.Errorf("edit: %w", err)
	}
	body, err := os.ReadFile(resolved)
	if err != nil {
		return turn.Result{}, fmt.Errorf("edit: %w", err)
	}

	if bytes.IndexByte(body, 0) >= 0 {
		return turn.Result{}, errors.New("edit: " + search.Note(search.BinarySkipped,
			args.Path+" holds a null byte, so it is not text and replacing a stretch of it would corrupt it"))
	}

	before := string(body)
	switch occurrences := strings.Count(before, args.Old); occurrences {
	case 0:
		return turn.Result{}, fmt.Errorf("edit: %s holds no text matching old_string%s", args.Path, nearestLine(before, args.Old))
	case 1:
	default:
		return turn.Result{}, fmt.Errorf("edit: old_string appears %d times in %s and an edit must name exactly one: add the lines above or below it until it is unique",
			occurrences, args.Path)
	}

	edits, err := transform.Derive(before, strings.Replace(before, args.Old, args.New, 1))
	if err != nil {
		return turn.Result{}, fmt.Errorf("edit: %w", err)
	}
	preview, err := transform.Plan(string(e.root), filepath.ToSlash(args.Path), edits)
	if err != nil {
		return turn.Result{}, fmt.Errorf("edit: %w", err)
	}
	if err := transform.Commit(string(e.root), preview); err != nil {
		if errors.Is(err, transform.ErrStale) {
			return turn.Result{}, errors.New("edit: " + search.Note(search.Stale,
				args.Path+" is no longer the text edit read, so nothing was written: read it again and edit the text that is there now"))
		}
		return turn.Result{}, fmt.Errorf("edit: %w", err)
	}
	return turn.Result{Content: preview.Diff, Command: "edit " + args.Path}, nil
}

func nearestLine(before, old string) string {
	first, _, _ := strings.Cut(old, "\n")
	trimmed := strings.TrimSpace(first)
	if trimmed == "" {
		return ""
	}
	for number, line := range strings.Split(before, "\n") {
		if strings.TrimSpace(line) == trimmed {
			return fmt.Sprintf(", though line %d reads %q, so the difference is in the whitespace or in the lines after it", number+1, line)
		}
	}
	return ""
}
