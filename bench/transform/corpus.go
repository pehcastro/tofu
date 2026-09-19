package transform

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

type Write struct {
	Session          string
	Step             int
	Path             string
	Before           string
	Content          string
	ResultBytes      int
	CompletionTokens int
	AloneInStep      bool
}

type session struct {
	ID    string `json:"id"`
	Steps []struct {
		Index            int `json:"index"`
		CompletionTokens int `json:"completion_tokens"`
		ToolCalls        []struct {
			Tool string `json:"tool"`
			Args struct {
				Path    string `json:"path"`
				Content string `json:"content"`
			} `json:"args"`
			ResultBytes int `json:"result_bytes"`
		} `json:"tool_calls"`
	} `json:"steps"`
}

func Load(sessionDir, beforeDir string) ([]Write, int, error) {
	names, err := filepath.Glob(filepath.Join(sessionDir, "*.json"))
	if err != nil {
		return nil, 0, fmt.Errorf("listing %s: %w", sessionDir, err)
	}
	slices.Sort(names)
	var writes []Write
	for _, name := range names {
		raw, err := os.ReadFile(name)
		if err != nil {
			return nil, 0, fmt.Errorf("reading %s: %w", name, err)
		}
		var turn session
		if err := json.Unmarshal(raw, &turn); err != nil {
			return nil, 0, fmt.Errorf("parsing %s: %w", name, err)
		}
		seen := map[string]string{}
		for _, step := range turn.Steps {
			for _, call := range step.ToolCalls {
				if call.Tool != "write" {
					continue
				}
				before, ok := seen[call.Args.Path]
				if !ok {
					if before, err = pristine(beforeDir, call.Args.Path); err != nil {
						return nil, 0, err
					}
				}
				writes = append(writes, Write{
					Session:          turn.ID,
					Step:             step.Index,
					Path:             call.Args.Path,
					Before:           before,
					Content:          call.Args.Content,
					ResultBytes:      call.ResultBytes,
					CompletionTokens: step.CompletionTokens,
					AloneInStep:      len(step.ToolCalls) == 1,
				})
				seen[call.Args.Path] = call.Args.Content
			}
		}
	}
	return writes, len(names), nil
}

func pristine(beforeDir, path string) (string, error) {
	body, err := os.ReadFile(filepath.Join(beforeDir, filepath.FromSlash(path)))
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("reading the pristine %s: %w", path, err)
	}
	return string(body), nil
}
