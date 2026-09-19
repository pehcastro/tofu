package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"boji/internal/llm"
)

type Result struct {
	Content  string
	Command  string
	ExitCode *int
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

func (r Registry) lookup(name string) (Tool, bool) {
	tool, ok := r.byName[name]
	return tool, ok
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


func confine(root, requested string) (string, error) { return Root(root).Resolve(requested) }
