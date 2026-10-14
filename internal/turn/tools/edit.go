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

	"tofu/internal/konst"
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
		Description: "replaces one exact stretch of an existing file. " +
			"old_string is copied from the file character for character and appears exactly once; " +
			"a small file needs no read first, so edit from what search showed you. " +
			"replace_all true replaces every occurrence instead, which is how a name is renamed through a file. " +
			"an empty new_string deletes. " +
			"on a go file, symbol names one whole declaration instead, Resolve or Root.Resolve for a method, and new_string becomes all of it. " +
			"give old_string or symbol, not both. " +
			"returns a unified diff, ending on a .ts or .tsx file with tsc's errors: fix them before moving on. " +
			"write creates a file or replaces a whole one",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":        map[string]any{"type": "string"},
				"old_string":  map[string]any{"type": "string"},
				"new_string":  map[string]any{"type": "string"},
				"symbol":      map[string]any{"type": "string"},
				"replace_all": map[string]any{"type": "boolean"},
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
	All    bool   `json:"replace_all"`
}

func (e Edit) Run(ctx context.Context, raw json.RawMessage) (turn.Result, error) {
	var args editArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return turn.Result{}, fmt.Errorf("edit: arguments are not the expected shape: %w", err)
	}
	args.Old, args.New = turn.AsLF(args.Old), turn.AsLF(args.New)
	if args.Old == "" && args.Symbol == "" {
		return turn.Result{}, errors.New("edit: say what is replaced, either old_string for a stretch of text copied from the file or symbol for the whole of one go declaration: write creates a file")
	}
	if args.Old != "" && args.Symbol != "" {
		return turn.Result{}, errors.New("edit: old_string and symbol are the two ways to say what is replaced and a call gives exactly one: symbol replaces a whole go declaration and old_string replaces a stretch of text")
	}
	if args.All && args.Symbol != "" {
		return turn.Result{}, errors.New("edit: replace_all replaces every occurrence of old_string and symbol names one declaration, so a call gives replace_all with old_string only")
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
			return turn.Result{}, fmt.Errorf("edit: %w. edit changes a file that exists: write creates one", refusal)
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
	text, endings := turn.EndingsOf(before)
	var after, note string
	switch count := strings.Count(text, args.Old); {
	case args.Symbol != "":
		after, err = replaceDeclaration(e.root, target, args.Symbol, text, args.New)
	case args.All && count > 1:
		after, note = strings.ReplaceAll(text, args.Old, args.New), fmt.Sprintf("replaced %d occurrences of old_string", count)
	default:
		after, note, err = replaceOnce(text, target, args.Old, args.New)
	}
	var change turn.LineChange
	if err == nil {
		change = turn.ChangeOf(text, after)
	}
	if !e.ledger.Saw(target, body) {
		if err != nil || args.Symbol != "" || e.ledger.Known(target) || len(body) > konst.RefusalCarriesWholeUpToBytes {
			return turn.Result{}, fmt.Errorf("edit: %s has not been read in this session or has changed since, so the edit is refused rather than trusted against a guess: %s",
				target, e.ledger.Refuse(target, body, change))
		}
		e.ledger.Mark(target, body)
	}
	if err != nil {
		return turn.Result{}, err
	}
	if err := e.ledger.Unshown(target, body, change); err != nil {
		return turn.Result{}, fmt.Errorf("edit: %w", err)
	}
	after = endings.Restore(after)
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
	e.ledger.Rewrote(target, []byte(preview.After), change)
	return turn.Result{
		Content: e.checkers.Typechecked(ctx, resolved, strings.Join(append(repairs, preview.Diff), "\n")),
		Command: target,
	}, nil
}
