package whatif

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"tofu/internal/models/llm/core"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/files/edit"
	"tofu/internal/tools/files/edit/patch"
)

type Whatif struct {
	root   contract.Root
	limits Limits
}

func New(dir string, limits Limits) (Whatif, error) {
	root, err := contract.NewRoot(dir)
	return Whatif{root: root, limits: limits}, err
}

func (Whatif) Name() string { return "whatif" }

func (Whatif) Definition() core.Tool {
	text := map[string]any{"type": "string"}
	change := map[string]any{"type": "object", "required": []string{"path", "new_string"}, "properties": map[string]any{
		"path": text, "old_string": text, "new_string": text, "symbol": text, "replace_all": map[string]any{"type": "boolean"},
	}}
	return core.Tool{
		Name: "whatif",
		Description: "applies edits to copies of the files and runs a check on them, and the working tree never changes, so a crash leaves nothing half changed and two agents can try rival fixes on one file. " +
			"edits take the shape of edit's edits, and an edit to a path that does not exist yet, with old_string empty, creates that file, so a new package can be tried. check build runs go build and go vet on the go package at path, or cargo build on the rust crate at or above it, or zig build where a build.zig is at or above it; " +
			"check test runs go test on that package, cargo test on that crate or zig build test, name filtering the go and rust tests; check command runs command, a program and its arguments, in a copy of the project at path. " +
			"go runs on the real tree through go's -overlay, a go package the edits create and the rest on a copy under the scratchpad that holds only the files git tracks or does not ignore. " +
			"returns passed, the check's digest and the diff of the edits. a check that runs past its limit says it timed out",
		Parameters: map[string]any{"type": "object", "required": []string{"edits", "check"}, "properties": map[string]any{
			"edits":   map[string]any{"type": "array", "items": change},
			"check":   map[string]any{"type": "string", "enum": []string{string(Build), string(Test), string(Command)}},
			"path":    text,
			"name":    text,
			"command": map[string]any{"type": "array", "items": text},
		}},
	}
}

func (w Whatif) Run(ctx context.Context, raw json.RawMessage) (contract.Result, error) {
	var args struct {
		Check
		Edits []edit.EditChange `json:"edits"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return contract.Result{}, fmt.Errorf("whatif: arguments are not the expected shape: %w", err)
	}
	files, diff, err := w.applied(args.Edits)
	if err != nil {
		return contract.Result{}, err
	}
	answer, err := Try(ctx, w.root, files, args.Check, w.limits)
	if err != nil {
		return contract.Result{}, err
	}
	result := contract.Result{Content: "passed: true"}
	if answer.Outcome != Passed {
		result = contract.Failure("passed: false, " + answer.Outcome.String())
	}
	result.Command = string(args.Kind)
	result.Content += "; the working tree was not changed\ncheck: " + answer.Digest + "\ncheck sha256: " + answer.Hash + "\ndiff:\n" + diff
	return result, nil
}

func (w Whatif) applied(changes []edit.EditChange) (map[string]string, string, error) {
	if len(changes) == 0 {
		return nil, "", errors.New("whatif: edits is empty, so there is nothing to try: give the edits as edit takes them")
	}
	files, before := map[string]string{}, map[string]string{}
	var order []string
	for _, change := range changes {
		target := path.Clean(filepath.ToSlash(change.Path))
		if !filepath.IsLocal(target) {
			return nil, "", fmt.Errorf("whatif: path %q is not inside the project", change.Path)
		}
		if _, read := files[target]; !read {
			resolved, err := w.root.Resolve(target)
			if err != nil {
				return nil, "", fmt.Errorf("whatif: %w", err)
			}
			body, err := os.ReadFile(resolved)
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				return nil, "", fmt.Errorf("whatif: %w", err)
			}
			files[target], before[target], order = string(body), string(body), append(order, target)
		}
		text, endings := contract.EndingsOf(files[target])
		change.Old, change.New = contract.AsLF(change.Old), contract.AsLF(change.New)
		after, _, err := edit.Apply(w.root, target, text, change)
		if err != nil {
			return nil, "", err
		}
		files[target] = endings.Restore(after)
	}
	diffs := make([]string, len(order))
	for i, target := range order {
		diffs[i] = patch.Unified(target, before[target], files[target])
	}
	return files, strings.Join(diffs, "\n"), nil
}
