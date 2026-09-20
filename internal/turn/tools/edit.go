package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"tofu/internal/llm"
	"tofu/internal/search"
	"tofu/internal/transform"
	"tofu/internal/turn"
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
			"and it has to appear exactly once: when it appears more than once the result lists every occurrence with its line number, " +
			"so add the lines above or below it until it is unique. " +
			"new_string is what it becomes, and an empty new_string deletes it. " +
			"a path that does not exist, or a stretch of text that differs from the file in whitespace alone, is repaired when there is exactly one candidate and refused when there are two, " +
			"and a repair is named at the top of the result. " +
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
	target := filepath.ToSlash(args.Path)
	var repairs []string
	body, err := os.ReadFile(resolved)
	if errors.Is(err, fs.ErrNotExist) {
		repaired, note, refusal := repairPath(e.root, args.Path, false)
		if refusal != nil {
			return turn.Result{}, fmt.Errorf("edit: %w", refusal)
		}
		target, repairs = repaired, append(repairs, note)
		if resolved, err = e.root.Resolve(target); err == nil {
			body, err = os.ReadFile(resolved)
		}
	}
	if err != nil {
		return turn.Result{}, fmt.Errorf("edit: %w", err)
	}

	if bytes.IndexByte(body, 0) >= 0 {
		return turn.Result{}, errors.New("edit: " + search.Note(search.BinarySkipped,
			target+" holds a null byte, so it is not text and replacing a stretch of it would corrupt it"))
	}

	before := string(body)
	after, note, err := replaceOnce(before, target, args.Old, args.New)
	if err != nil {
		return turn.Result{}, err
	}
	if note != "" {
		repairs = append(repairs, note)
	}

	edits, err := transform.Derive(before, after)
	if err != nil {
		return turn.Result{}, fmt.Errorf("edit: %w", err)
	}
	preview, err := transform.Plan(string(e.root), target, edits)
	if err != nil {
		return turn.Result{}, fmt.Errorf("edit: %w", err)
	}
	if err := transform.Commit(string(e.root), preview); err != nil {
		if errors.Is(err, transform.ErrStale) {
			return turn.Result{}, errors.New("edit: " + search.Note(search.Stale,
				target+" is no longer the text edit read, so nothing was written: read it again and edit the text that is there now"))
		}
		return turn.Result{}, fmt.Errorf("edit: %w", err)
	}
	return turn.Result{
		Content: strings.Join(append(repairs, preview.Diff), "\n"),
		Command: "edit " + target,
	}, nil
}
