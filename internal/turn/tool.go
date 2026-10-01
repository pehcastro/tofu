package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"tofu/internal/konst"
	"tofu/internal/llm"
)

type ResultOutcome int

const (
	ResultUnset ResultOutcome = iota
	ResultSucceeded
	ResultFailed
	ResultAborted
)

type Result struct {
	Content     string
	Command     string
	ExitCode    *int
	FailureText string
	Outcome     ResultOutcome
	SubAgent    string
}

const resultCapMarker = "\n...(%s dropped from the middle of this result, cut at a %d byte cap.)...\n"

func capResult(content string) string {
	if len(content) <= konst.TurnResultBytesCap {
		return content
	}
	head := runeSafeHead(content, konst.TurnResultBytesCap/2)
	tail := runeSafeTail(content, konst.TurnResultBytesCap-len(head))
	dropped := len(content) - len(head) - len(tail)
	amount := fmt.Sprintf("%d bytes", dropped)
	if dropped == 1 {
		amount = "1 byte"
	}
	return head + fmt.Sprintf(resultCapMarker, amount, konst.TurnResultBytesCap) + tail
}

func runeSafeHead(s string, n int) string {
	if n >= len(s) {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func runeSafeTail(s string, n int) string {
	if n >= len(s) {
		return s
	}
	start := len(s) - n
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	return s[start:]
}

type Tool interface {
	Name() string
	Definition() llm.Tool
	Run(ctx context.Context, args json.RawMessage) (Result, error)
}

type Registry struct {
	tools  []Tool
	byName map[string]Tool
}

func NewRegistry(tools ...Tool) Registry {
	byName := make(map[string]Tool, len(tools))
	for _, tool := range tools {
		byName[tool.Name()] = tool
	}
	return Registry{tools: tools, byName: byName}
}

func readOnly(name string) bool {
	switch name {
	case "read", "glob", "search", "symbols", "project_report", "artifact_fetch", "fetch", "web_search", "github_pr_diff", "typecheck", "test":
		return true
	}
	return false
}

func (r Registry) parallelPrefix(calls []llm.ToolCall) int {
	if spawner, spawning := r.byName["spawn"].(*SpawnTool); spawning && calls[0].Name == "spawn" {
		return spawner.disjointPrefix(calls)
	}
	width := min(len(calls), konst.TurnParallelToolCalls)
	for i, call := range calls[:width] {
		if _, known := r.byName[call.Name]; !known || !readOnly(call.Name) {
			return i
		}
	}
	return width
}

func (r Registry) Definitions() []llm.Tool {
	defs := make([]llm.Tool, len(r.tools))
	for i, tool := range r.tools {
		defs[i] = tool.Definition()
	}
	return defs
}

type Root string

func NewRoot(dir string) (Root, error) {
	if dir == "" {
		return "", errors.New("turn: a tool needs a working directory")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("turn: working directory %q: %w", dir, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("turn: working directory %q: %w", abs, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("turn: working directory %q is not a directory", abs)
	}
	return Root(abs), nil
}

func (r Root) Resolve(requested string) (string, error) {
	if strings.TrimSpace(requested) == "" {
		return "", errors.New("path is required")
	}
	if filepath.IsAbs(requested) {
		return "", fmt.Errorf("path %q must be relative to the turn's working directory", requested)
	}
	cleaned := filepath.Clean(filepath.Join(string(r), requested))
	rel, err := filepath.Rel(string(r), cleaned)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes the turn's working directory", requested)
	}
	return cleaned, nil
}
