package tools

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"strings"

	"boji/internal/llm"
	"boji/internal/turn"
)

type Glob struct {
	root turn.Root
}

func NewGlob(dir string) (Glob, error) {
	root, err := turn.NewRoot(dir)
	return Glob{root: root}, err
}

func (g Glob) Name() string { return "glob" }

func (g Glob) Definition() llm.Tool {
	return llm.Tool{
		Name: "glob",
		Description: "lists the files under the turn's working directory whose path matches a shell pattern. " +
			"the pattern is matched against the whole path relative to the working directory and against the file name alone, " +
			"so *.ts finds every typescript file at any depth and src/*.ts finds only the ones directly under src. " +
			"it never descends into node_modules, .git or .boji. " +
			"it does not read a file and it does not search file contents: grep does that",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{"type": "string"},
				"path":    map[string]any{"type": "string"},
			},
			"required": []string{"pattern"},
		},
	}
}

type globArgs struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path"`
}

func (g Glob) Run(_ context.Context, raw json.RawMessage) (turn.Result, error) {
	var args globArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return turn.Result{}, fmt.Errorf("glob: arguments are not the expected shape: %w", err)
	}
	if strings.TrimSpace(args.Pattern) == "" {
		return turn.Result{}, errors.New("glob: pattern is required")
	}
	if _, err := path.Match(args.Pattern, "probe"); err != nil {
		return turn.Result{}, fmt.Errorf("glob: %q is not a shell pattern: %w", args.Pattern, err)
	}
	under := cmp.Or(args.Path, ".")
	files, err := filesUnder(g.root, under)
	if err != nil {
		return turn.Result{}, fmt.Errorf("glob: %w", err)
	}

	var found []string
	for _, rel := range files {
		if matchesPattern(args.Pattern, rel) {
			found = append(found, rel)
		}
	}

	command := "glob " + args.Pattern + " under " + under
	if len(found) == 0 {
		return turn.Result{
			Content: fmt.Sprintf("no file under %s matches %q, out of %d files searched. the directory exists and was read: this is an answer, not a failure",
				under, args.Pattern, len(files)),
			Command: command,
		}, nil
	}
	return turn.Result{
		Content: fmt.Sprintf("%d of %d files under %s match %q\n%s\n", len(found), len(files), under, args.Pattern, strings.Join(found, "\n")),
		Command: command,
	}, nil
}

func matchesPattern(pattern, rel string) bool {
	if ok, _ := path.Match(pattern, rel); ok {
		return true
	}
	ok, _ := path.Match(pattern, path.Base(rel))
	return ok
}

func filesUnder(root turn.Root, under string) ([]string, error) {
	from, err := root.Resolve(under)
	if err != nil {
		return nil, err
	}
	var files []string
	walkErr := filepath.WalkDir(from, func(full string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case "node_modules", ".git", ".boji":
				return fs.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(string(root), full)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	return files, walkErr
}
