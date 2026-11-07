package trail

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"tofu/internal/tools/files/edit/patch"
	"tofu/internal/tools/history/shadow"
)

type pending struct {
	blob, mode string
	planned    bool
	merged     *string
}

func (t Trail) merge(ctx context.Context, at *pending, c shadow.Change) ([]string, error) {
	regular := func(mode string) bool { return mode == "100644" || mode == "100755" }
	if !regular(c.FromMode) || !regular(c.ToMode) || at.blob == shadow.AbsentBlob {
		return []string{fmt.Sprintf("it is %s now and the turn left it %s, and only a text file present on both sides merges", short(at.blob), short(c.ToBlob))}, nil
	}
	blob := func(hash string) (string, error) { return t.Store.Git(ctx, "", "cat-file", "blob", hash) }
	raw, err := os.ReadFile(filepath.Join(t.Store.Work(), filepath.FromSlash(c.Path)))
	current := string(raw)
	switch {
	case at.merged != nil:
		current, err = *at.merged, nil
	case at.planned:
		current, err = blob(at.blob)
	}
	before, beforeErr := blob(c.FromBlob)
	after, afterErr := blob(c.ToBlob)
	if err := errors.Join(err, beforeErr, afterErr); err != nil {
		return nil, err
	}
	merged, clashes := undo(after, current, before)
	if len(clashes) > 0 {
		return clashes, nil
	}
	hash, err := t.Store.Git(ctx, merged, "hash-object", "--stdin")
	*at = pending{blob: strings.TrimSpace(hash), mode: c.FromMode, planned: true, merged: &merged}
	return nil, err
}

func undo(after, now, before string) (string, []string) {
	end := func(h patch.Hunk) int { return h.BeforeStart + len(h.Removed) }
	lines := func(from, count int) string {
		if count <= 1 {
			return fmt.Sprintf("line %d", from+1)
		}
		return fmt.Sprintf("lines %d-%d", from+1, from+count)
	}
	later := patch.Hunks(after, now)
	reverted := slices.DeleteFunc(patch.Hunks(after, before), func(h patch.Hunk) bool {
		return slices.ContainsFunc(later, func(kept patch.Hunk) bool {
			return kept.BeforeStart == h.BeforeStart && slices.Equal(kept.Removed, h.Removed) && slices.Equal(kept.Added, h.Added)
		})
	})
	var clashes []string
	for _, mine := range reverted {
		for _, theirs := range later {
			if mine.BeforeStart <= end(theirs) && theirs.BeforeStart <= end(mine) {
				clashes = append(clashes, fmt.Sprintf("the turn wrote %s, and a later change rewrote it as %s of the file now", lines(mine.BeforeStart, len(mine.Removed)), lines(theirs.AfterStart, len(theirs.Added))))
			}
		}
	}
	if len(clashes) > 0 {
		return "", clashes
	}
	all := slices.SortedFunc(slices.Values(append(later, reverted...)), func(a, b patch.Hunk) int { return a.BeforeStart - b.BeforeStart })
	base := strings.SplitAfter(after, "\n")
	var out strings.Builder
	at := 0
	for _, h := range all {
		out.WriteString(strings.Join(base[at:h.BeforeStart], "") + strings.Join(h.Added, ""))
		at = end(h)
	}
	out.WriteString(strings.Join(base[at:], ""))
	return out.String(), nil
}
