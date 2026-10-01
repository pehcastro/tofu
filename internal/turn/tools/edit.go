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
	root     turn.Root
	ledger   *turn.ReadLedger
	checkers *turn.Typecheckers
}

func NewEdit(dir string) (Edit, error) {
	root, err := turn.NewRoot(dir)
	return Edit{root: root}, err
}

func (e Edit) Reading(ledger *turn.ReadLedger) Edit {
	e.ledger = ledger
	return e
}

func (e Edit) Checking(checkers *turn.Typecheckers) Edit {
	e.checkers = checkers
	return e
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
			"the result is a unified diff of what changed, and on a .ts or .tsx file it ends with the errors the project's own tsc finds in that file: fix them before moving on. " +
			"symbol is the other way to say what is replaced, on a go file only: it names one declaration, spelled Resolve for a function, a type or a value and Root.Resolve for a method, " +
			"and new_string becomes the whole of it, so the old text is never copied. " +
			"use symbol when a whole declaration is being rewritten and old_string when part of one is or the file is not go. " +
			"exactly one of the two is given. a symbol that does not name exactly one declaration in that file is refused with what was found, " +
			"and an edit that would leave a go file unable to parse is refused with nothing written. " +
			"use write instead to create a file or to replace the whole of one",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":       map[string]any{"type": "string"},
				"old_string": map[string]any{"type": "string"},
				"new_string": map[string]any{"type": "string"},
				"symbol":     map[string]any{"type": "string"},
			},
			"required": []string{"path", "new_string"},
		},
	}
}

type editArgs struct {
	Path   string `json:"path"`
	Old    string `json:"old_string"`
	New    string `json:"new_string"`
	Symbol string `json:"symbol"`
}

func (e Edit) Run(ctx context.Context, raw json.RawMessage) (turn.Result, error) {
	var args editArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return turn.Result{}, fmt.Errorf("edit: arguments are not the expected shape: %w", err)
	}
	if args.Old == "" && args.Symbol == "" {
		return turn.Result{}, errors.New("edit: say what is replaced, either old_string for a stretch of text copied from the file or symbol for the whole of one go declaration: write creates a file")
	}
	if args.Old != "" && args.Symbol != "" {
		return turn.Result{}, errors.New("edit: old_string and symbol are the two ways to say what is replaced and a call gives exactly one: symbol replaces a whole go declaration and old_string replaces a stretch of text")
	}
	if args.Old != "" && args.Old == args.New {
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
	if !e.ledger.Saw(target, body) {
		return turn.Result{}, fmt.Errorf("edit: %s has not been read in this session or has changed since, so the edit is refused rather than trusted against a guess: "+
			"its current content follows, so edit the text that is actually there.\n%s", target, e.ledger.Refuse(target, body))
	}

	before := string(body)
	var after, note string
	if args.Symbol != "" {
		after, err = replaceDeclaration(e.root, target, args.Symbol, before, args.New)
	} else {
		after, note, err = replaceOnce(before, target, args.Old, args.New)
	}
	if err != nil {
		return turn.Result{}, err
	}
	if note != "" {
		repairs = append(repairs, note)
	}
	if err := goStillParses(target, before, after); err != nil {
		return turn.Result{}, err
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
	e.ledger.Mark(target, []byte(preview.After))
	return turn.Result{
		Content: e.checkers.Typechecked(ctx, resolved, strings.Join(append(repairs, preview.Diff), "\n")),
		Command: target,
	}, nil
}
