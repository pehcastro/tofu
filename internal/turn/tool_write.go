package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/sys"
	"tofu/internal/transform"
)

const writePerm = 0o644

type ReadLedger struct {
	mutex sync.Mutex
	seen  map[string]bool
}

func NewReadLedger() *ReadLedger {
	return &ReadLedger{seen: map[string]bool{}}
}

func ledgerKey(path string) string {
	return filepath.ToSlash(filepath.Clean(path))
}

func (l *ReadLedger) Mark(path string) {
	if l == nil {
		return
	}
	l.mutex.Lock()
	defer l.mutex.Unlock()
	l.seen[ledgerKey(path)] = true
}

func (l *ReadLedger) Saw(path string) bool {
	if l == nil {
		return true
	}
	l.mutex.Lock()
	defer l.mutex.Unlock()
	return l.seen[ledgerKey(path)]
}

func RefusalExcerpt(body []byte) string {
	if len(body) <= konst.TurnResultBytesCap {
		return string(body)
	}
	head := konst.TurnResultBytesCap / 2
	tailFrom := len(body) - (konst.TurnResultBytesCap - head)
	return string(body[:head]) +
		fmt.Sprintf("\n...(%d bytes cut from the middle, too large for a refusal to carry whole)...\n", tailFrom-head) +
		string(body[tailFrom:])
}

type WriteTool struct {
	root   Root
	ledger *ReadLedger
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

func (t *WriteTool) Name() string { return "write" }

func (t *WriteTool) Definition() llm.Tool {
	return llm.Tool{
		Name: "write",
		Description: "writes a file inside the turn's working directory, replacing it whole. " +
			"the result says the file was created when it was not there before, " +
			"and is a unified diff of what changed when it was",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    map[string]any{"type": "string"},
				"content": map[string]any{"type": "string"},
			},
			"required": []string{"path", "content"},
		},
	}
}

type writeArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (t *WriteTool) Run(_ context.Context, raw json.RawMessage) (Result, error) {
	var args writeArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("write: arguments are not the expected shape: %w", err)
	}
	resolved, err := t.root.Resolve(args.Path)
	if err != nil {
		return Result{}, fmt.Errorf("write: %w", err)
	}
	held, readErr := os.ReadFile(resolved)
	if readErr != nil && !errors.Is(readErr, fs.ErrNotExist) {
		return Result{}, fmt.Errorf("write: reading %s before replacing it: %w", args.Path, readErr)
	}
	if readErr == nil && !t.ledger.Saw(args.Path) {
		return Result{}, fmt.Errorf("write: %s exists and has not been read by this turn, so replacing it whole is refused rather than trusted against a guess: "+
			"read it, or edit part of it, then write it again with its current content folded in.\n%s", args.Path, RefusalExcerpt(held))
	}
	if err := sys.WriteFile(resolved, []byte(args.Content), writePerm); err != nil {
		return Result{}, fmt.Errorf("write: %w", err)
	}
	t.ledger.Mark(args.Path)
	before := string(held)
	preview := transform.Preview{
		Path:    args.Path,
		Existed: readErr == nil,
		Before:  before,
		After:   args.Content,
		Diff:    transform.Unified(args.Path, before, args.Content),
	}
	return Result{Content: preview.Result(), Command: args.Path}, nil
}
