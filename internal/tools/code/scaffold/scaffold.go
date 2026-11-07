package scaffold

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

	"tofu/internal/models/llm/core"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/files/edit/patch"
)

const Name = "scaffold"

type Scaffold struct{ root contract.Root }

func New(dir string) (contract.Tool, error) {
	root, err := contract.NewRoot(dir)
	return Scaffold{root: root}, err
}

func (Scaffold) Name() string { return Name }

func (Scaffold) Definition() core.Tool {
	return core.Tool{
		Name: Name,
		Description: "makes a new file that looks like an existing sibling: it copies the file named in like, beside it, with the sibling's name replaced by the new name " +
			"in every case form it appears in, PascalCase, camelCase, snake_case, kebab-case and SCREAMING_SNAKE, in the contents and in the file name. " +
			"use it instead of reading two siblings and writing a third by hand. kind says what is made, such as component, handler or test. " +
			"the sibling's name is its file name before the first dot, so like src/order.ts with name Invoice writes src/invoice.ts. " +
			"a name inside a longer word, as order in reorder or orders, is left alone. a target that already exists is refused and nothing is written. " +
			"returns the file written as a diff and each form replaced with its count",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"kind": map[string]any{"type": "string"},
				"name": map[string]any{"type": "string"},
				"like": map[string]any{"type": "string"},
			},
			"required": []string{"kind", "name", "like"},
		},
	}
}

func (s Scaffold) Run(_ context.Context, raw json.RawMessage) (contract.Result, error) {
	var args struct {
		Kind string `json:"kind"`
		Name string `json:"name"`
		Like string `json:"like"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return contract.Result{}, fmt.Errorf("scaffold: arguments are not the expected shape: %w", err)
	}
	kind, like := strings.TrimSpace(args.Kind), strings.TrimSpace(args.Like)
	switch {
	case like == "":
		return contract.Failure("scaffold: like is required: name the sibling file to copy, as \"like\": \"src/components/Sidebar.tsx\". nothing written"), nil
	case kind == "":
		return contract.Failure("scaffold: kind is required: say what is made, such as component. nothing written"), nil
	}
	newWords, err := words(args.Name)
	if err != nil {
		return contract.Failure("scaffold: name " + err.Error() + ". nothing written"), nil
	}
	sibling, err := s.root.Resolve(like)
	if err != nil {
		return contract.Failure("scaffold: like " + err.Error() + ". nothing written"), nil
	}
	info, err := os.Stat(sibling)
	switch {
	case err != nil:
		return contract.Failure("scaffold: like " + like + " cannot be read: " + err.Error() + ". nothing written"), nil
	case info.IsDir():
		return contract.Failure("scaffold: like " + like + " is a directory: name one file to copy. nothing written"), nil
	}
	base := filepath.Base(sibling)
	stem, _, _ := strings.Cut(base, ".")
	oldWords, err := words(stem)
	if err != nil {
		return contract.Failure("scaffold: the sibling's file name " + base + " holds no name to replace: " + err.Error() + ". nothing written"), nil
	}
	pairs := renames(oldWords, newWords)
	targetBase, _ := replaced(base, pairs)
	target := filepath.Join(filepath.Dir(sibling), targetBase)
	relative, _ := filepath.Rel(string(s.root), target)
	relative = filepath.ToSlash(relative)
	before, err := os.ReadFile(sibling)
	if err != nil {
		return contract.Result{}, fmt.Errorf("scaffold: %w", err)
	}
	after, counts := replaced(string(before), pairs)
	err = createOnly(target, after, info.Mode().Perm())
	if errors.Is(err, fs.ErrExist) {
		return contract.Failure("scaffold: " + relative + " already exists, so nothing was written. pick another name, or edit that file"), nil
	}
	if err != nil {
		return contract.Result{}, fmt.Errorf("scaffold: wrote %s: %w", relative, err)
	}
	head := fmt.Sprintf("scaffold: %s %s like %s, wrote %s\n", kind, args.Name, filepath.ToSlash(like), relative)
	return contract.Result{Content: head + summary(stem, pairs, counts) + patch.Unified(relative, "", after), Command: relative}, nil
}

func createOnly(path, data string, perm fs.FileMode) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	_, err = file.WriteString(data)
	if err = cmp.Or(err, file.Close()); err != nil {
		_ = os.Remove(path)
	}
	return err
}

func summary(stem string, pairs []rename, counts []int) string {
	var out strings.Builder
	for i, pair := range pairs {
		if counts[i] > 0 {
			fmt.Fprintf(&out, "replaced %s -> %s (%s): %d in the contents\n", pair.old, pair.new, pair.forms, counts[i])
		}
	}
	if out.Len() == 0 {
		return "no form of " + stem + " appears in the contents, so they are copied unchanged\n"
	}
	return out.String()
}
