package contract

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"tofu/internal/librarian/rules"
	"tofu/internal/models/llm/core"
	"tofu/internal/sys"
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
	Images      []llm.Image
	Command     string
	ExitCode    *int
	Hits        *int
	FailureText string
	Outcome     ResultOutcome
	SubAgent    string
	Repeat      bool
	Origin      llm.Origin
	Rules       []rule.Fire
}

type Tool interface {
	Name() string
	Definition() llm.Tool
	Run(ctx context.Context, args json.RawMessage) (Result, error)
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
	if filepath.IsAbs(requested) || strings.HasPrefix(filepath.ToSlash(requested), "/") {
		return r.inScratch(requested)
	}
	cleaned := filepath.Clean(filepath.Join(string(r), requested))
	rel, inside := Within(string(r), cleaned)
	if !inside {
		return "", fmt.Errorf("path %q escapes the turn's working directory", requested)
	}
	realRoot, _, err := followLinks(filepath.VolumeName(string(r))+string(filepath.Separator), string(r))
	if err != nil {
		return "", fmt.Errorf("turn: working directory %q: %w", string(r), err)
	}
	resolved, firstLink, err := followLinks(realRoot, rel)
	if err != nil {
		return "", fmt.Errorf("path %q: %w", requested, err)
	}
	if _, inside := Within(realRoot, resolved); !inside {
		return "", fmt.Errorf("path %q goes through the link %q to %q, outside the turn's working directory", requested, firstLink, resolved)
	}
	return cleaned, nil
}

func (r Root) inScratch(requested string) (string, error) {
	scratch, err := sys.ScratchRootAt(string(r))
	cleaned := filepath.Clean(requested)
	if _, inside := Within(scratch, cleaned); err != nil || !filepath.IsAbs(requested) || !inside {
		return "", fmt.Errorf("path %q must be relative to the turn's working directory, or inside this project's scratchpad", requested)
	}
	top := filepath.VolumeName(scratch) + string(filepath.Separator)
	realScratch, _, err := followLinks(top, scratch)
	if err != nil {
		return "", fmt.Errorf("the scratchpad %q: %w", scratch, err)
	}
	resolved, firstLink, err := followLinks(top, cleaned)
	if err != nil {
		return "", fmt.Errorf("path %q: %w", requested, err)
	}
	if _, inside := Within(realScratch, resolved); !inside {
		return "", fmt.Errorf("path %q goes through the link %q to %q, outside the scratchpad", requested, firstLink, resolved)
	}
	return cleaned, nil
}

func Within(base, path string) (string, bool) {
	rel, err := filepath.Rel(base, path)
	return rel, err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func followLinks(from, path string) (resolved, firstLink string, err error) {
	current := from
	pending := splitPath(path[len(filepath.VolumeName(path)):])
	followed := map[string]bool{}
	for len(pending) > 0 {
		next := filepath.Join(current, pending[0])
		pending = pending[1:]
		info, err := os.Lstat(next)
		if errors.Is(err, fs.ErrNotExist) {
			return filepath.Join(append([]string{next}, pending...)...), firstLink, nil
		}
		if err != nil {
			return "", firstLink, err
		}
		if info.Mode()&(os.ModeSymlink|os.ModeIrregular) == 0 {
			current = next
			continue
		}
		target, err := os.Readlink(next)
		if err != nil && info.Mode()&os.ModeSymlink == 0 {
			current = next
			continue
		}
		if err != nil {
			return "", firstLink, err
		}
		name, _ := filepath.Rel(from, next)
		if followed[next] {
			return "", firstLink, fmt.Errorf("the link %q is a loop", name)
		}
		followed[next] = true
		firstLink = cmp.Or(firstLink, name)
		volume := filepath.VolumeName(target)
		if volume != "" || strings.HasPrefix(target, "/") || strings.HasPrefix(target, string(filepath.Separator)) {
			current = cmp.Or(volume, filepath.VolumeName(current)) + string(filepath.Separator)
		}
		pending = append(splitPath(target[len(volume):]), pending...)
	}
	return current, firstLink, nil
}

func splitPath(path string) []string {
	return strings.FieldsFunc(path, func(c rune) bool { return c == filepath.Separator || c == '/' })
}
