package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"tofu/internal/llm"
	"tofu/internal/sys"
	"tofu/internal/transform"
)

const writePerm = 0o644

type WriteTool struct {
	root     Root
	ledger   *ReadLedger
	checkers *Typecheckers
}

func NewWriteTool(root string) (*WriteTool, error) {
	resolved, err := NewRoot(root)
	if err != nil {
		return nil, err
	}
	return &WriteTool{root: resolved}, nil
}

func (t *WriteTool) Reading(ledger *ReadLedger) *WriteTool {
	t.ledger = ledger
	return t
}

func (t *WriteTool) Checking(checkers *Typecheckers) *WriteTool {
	t.checkers = checkers
	return t
}

func (t *WriteTool) Name() string { return "write" }

func (t *WriteTool) Definition() llm.Tool {
	return llm.Tool{
		Name: "write",
		Description: "writes a file inside the turn's working directory, replacing it whole. " +
			"the result says the file was created when it was not there before, " +
			"and is a unified diff of what changed when it was. " +
			"a .ts or .tsx file is then typechecked with the project's own tsc, and its errors end the result: fix them before moving on. " +
			"on a project too large to recheck at once, the result says the check runs in the background and shows the errors its last check found. " +
			"with append true the content goes after the end of a file read in this session, with a newline between when the file has none at its end, and the result shows the appended lines numbered",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    map[string]any{"type": "string"},
				"content": map[string]any{"type": "string"},
				"append":  map[string]any{"type": "boolean"},
			},
			"required": []string{"path", "content"},
		},
	}
}

type writeArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Append  bool   `json:"append"`
}

func (t *WriteTool) Run(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args writeArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("write: arguments are not the expected shape: %w", err)
	}
	resolved, err := t.root.Resolve(args.Path)
	if err != nil {
		return Result{}, fmt.Errorf("write: %w", err)
	}
	if args.Append {
		return t.appendTo(ctx, resolved, args)
	}
	held, readErr := os.ReadFile(resolved)
	if readErr != nil && !errors.Is(readErr, fs.ErrNotExist) {
		return Result{}, fmt.Errorf("write: reading %s before replacing it: %w", args.Path, readErr)
	}
	if readErr != nil {
		err = sys.WriteFile(resolved, []byte(args.Content), writePerm)
	} else {
		change := ChangeOf(string(held), args.Content)
		if !t.ledger.Saw(args.Path, held) {
			return Result{}, fmt.Errorf("write: %s exists and has not been read in this session or has changed since, so replacing it whole is refused rather than trusted against a guess: %s",
				args.Path, t.ledger.Refuse(args.Path, held, change))
		}
		if err := t.ledger.Unshown(args.Path, held, change); err != nil {
			return Result{}, fmt.Errorf("write: %w", err)
		}
		err = overwrite(resolved, held, []byte(args.Content))
	}
	if err != nil {
		return Result{}, fmt.Errorf("write: %w", err)
	}
	t.ledger.Mark(args.Path, []byte(args.Content))
	before := string(held)
	preview := transform.Preview{
		Path:    args.Path,
		Existed: readErr == nil,
		Before:  before,
		After:   args.Content,
		Diff:    transform.Unified(args.Path, before, args.Content),
	}
	return Result{Content: t.checkers.Typechecked(ctx, resolved, preview.Result()), Command: args.Path}, nil
}

func (t *WriteTool) appendTo(ctx context.Context, resolved string, args writeArgs) (Result, error) {
	if args.Content == "" {
		return Result{}, fmt.Errorf("write: append with empty content would change nothing in %s, so it is refused", args.Path)
	}
	held, err := os.ReadFile(resolved)
	if errors.Is(err, fs.ErrNotExist) {
		return Result{}, fmt.Errorf("write: %s does not exist, so there is no end to append to: write it without append to create it", args.Path)
	}
	if err != nil {
		return Result{}, fmt.Errorf("write: reading %s before appending to it: %w", args.Path, err)
	}
	lead, endings := EndingsOf(string(held))
	if len(held) > 0 && !strings.HasSuffix(lead, "\n") {
		lead += "\n"
	}
	added := strings.ReplaceAll(args.Content, "\r\n", "\n")
	after := endings.Restore(lead + added)
	change := ChangeOf(string(held), after)
	if !t.ledger.Saw(args.Path, held) {
		return Result{}, fmt.Errorf("write: %s has not been read in this session or has changed since, so appending to it is refused: %s",
			args.Path, t.ledger.Refuse(args.Path, held, change))
	}
	if err := t.ledger.Unshown(args.Path, held, change); err != nil {
		return Result{}, fmt.Errorf("write: %w", err)
	}
	if err := overwrite(resolved, held, []byte(after)); err != nil {
		return Result{}, fmt.Errorf("write: %w", err)
	}
	t.ledger.Rewrote(args.Path, []byte(after), change)
	first := strings.Count(lead, "\n") + 1
	lines := strings.Split(strings.TrimSuffix(added, "\n"), "\n")
	shown := fmt.Sprintf("appended %d lines to %s, lines %d-%d:", len(lines), args.Path, first, first+len(lines)-1)
	for i, line := range lines {
		shown += fmt.Sprintf("\n%d\t%s", first+i, line)
	}
	return Result{Content: t.checkers.Typechecked(ctx, resolved, shown), Command: args.Path}, nil
}

func overwrite(path string, held, content []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("opening it to write in place, so nothing changed: %w", err)
	}
	_, err = file.WriteAt(content, 0)
	if err == nil {
		err = file.Truncate(int64(len(content)))
	}
	if err == nil {
		return file.Close()
	}
	_, restoreErr := file.WriteAt(held, 0)
	if restoreErr == nil {
		restoreErr = file.Truncate(int64(len(held)))
	}
	_ = file.Close()
	if restoreErr != nil {
		return fmt.Errorf("%w, and putting the old content back failed too, so the file may hold part of each: %w", err, restoreErr)
	}
	return fmt.Errorf("%w, and the old content was put back", err)
}
