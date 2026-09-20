package transform

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

var ErrStale = errors.New("the file changed after the preview was taken")

type Preview struct {
	Path    string
	Existed bool
	Before  string
	After   string
	Diff    string
}

func Plan(root, path string, edits []Edit) (Preview, error) {
	if !filepath.IsLocal(path) {
		return Preview{}, fmt.Errorf("path %q leaves the root it was given", path)
	}
	raw, err := os.ReadFile(filepath.Join(root, path))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return Preview{}, fmt.Errorf("reading %s before the edit: %w", path, err)
	}
	existed := err == nil
	before := string(raw)
	after := before
	for i, edit := range edits {
		after, err = edit.On(after)
		if err != nil {
			return Preview{}, fmt.Errorf("edit %d of %d on %s: %w", i+1, len(edits), path, err)
		}
	}
	return Preview{Path: path, Existed: existed, Before: before, After: after, Diff: Unified(path, before, after)}, nil
}

func (p Preview) Result() string {
	if !p.Existed {
		return fmt.Sprintf("created %s: %d lines, %d bytes", p.Path, len(splitLines(p.After)), len(p.After))
	}
	if p.Diff == "" {
		return fmt.Sprintf("%s already held that text, so nothing changed", p.Path)
	}
	return p.Diff
}

func Commit(root string, p Preview) error {
	full := filepath.Join(root, p.Path)
	current, err := os.ReadFile(full)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("reading %s before the write: %w", p.Path, err)
	}
	if string(current) != p.Before {
		return fmt.Errorf("%s: %w, %d bytes on disk against %d previewed",
			p.Path, ErrStale, len(current), len(p.Before))
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		return fmt.Errorf("making the directory for %s: %w", p.Path, err)
	}
	if err := os.WriteFile(full, []byte(p.After), 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", p.Path, err)
	}
	return nil
}
