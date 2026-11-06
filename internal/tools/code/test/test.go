package test

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"tofu/internal/konst"
	"tofu/internal/models/llm/core"
	"tofu/internal/tools/code/lsp"
	"tofu/internal/tools/code/symbols"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/history/shadow"
)

type Test struct {
	root    contract.Root
	runners *TestRunners
	impact  symbols.Symbols
}

func NewTest(dir string, runners *TestRunners) (Test, error) {
	root, err := contract.NewRoot(dir)
	impact, impactErr := symbols.NewSymbols(dir)
	return Test{root: root, runners: runners, impact: impact}, cmp.Or(err, impactErr)
}

func (t Test) Serving(servers *lsp.Pool) Test {
	t.impact = t.impact.Serving(servers)
	return t
}

func (Test) Name() string { return "test" }

func (Test) Definition() core.Tool {
	return core.Tool{
		Name: "test",
		Description: "runs the tests for one path and returns the pass, fail and skip counts, each failure with its path:line and what it got against what it wanted, and the skipped tests by name. " +
			"the nearest project file above path picks the runner: go.mod runs go test on that one package, Cargo.toml runs cargo test on that crate, a python project runs pytest when it declares pytest and unittest when it does not, and package.json runs vitest, or node --test when its test script runs node --test or the test file imports node:test. " +
			"path is a directory, a test file, or a source file whose tests then run: the Test funcs in <stem>_test.go, test_<stem>.py or <stem>_test.py, the tests of that rust module, or the files beside it named <stem>.test.* or <stem>.spec.* for vitest. " +
			"name runs one go, rust, python or node --test test by name. repeat n runs it n times and says real when every run fails, flaky when some do, and passes when none do. " +
			"changed runs the tests of every file this turn changed instead of path, and also every test that symbols impact finds reaching a declaration the turn's edits touched, in any package, and says how many it found and why when it found none. " +
			"run tests with this instead of through the shell: it runs one package at a time on a bounded number of processors, and a vitest runner stays warm, so a run after the first answers in about a second",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    map[string]any{"type": "string"},
				"name":    map[string]any{"type": "string"},
				"repeat":  map[string]any{"type": "integer", "minimum": 1},
				"changed": map[string]any{"type": "boolean"},
			},
		},
	}
}

func (t Test) Run(ctx context.Context, raw json.RawMessage) (contract.Result, error) {
	var args struct {
		Path    string `json:"path"`
		Name    string `json:"name"`
		Repeat  int    `json:"repeat"`
		Changed bool   `json:"changed"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return contract.Result{}, fmt.Errorf("test: arguments are not the expected shape: %w", err)
	}
	targets := []testTarget{{path: args.Path, name: args.Name}}
	var reports []string
	if args.Changed {
		changed, said := t.changed(ctx)
		if said.Content != "" {
			return said, nil
		}
		targets = nil
		for _, path := range changed {
			targets = append(targets, testTarget{path: path, name: args.Name})
		}
		reached, why := t.reaching(ctx, changed)
		targets, reports = append(targets, reached...), []string{why}
	}
	result := contract.Result{Command: cmp.Or(args.Path, "changed")}
	for _, target := range targets {
		resolved, err := t.root.Resolve(target.path)
		if err != nil {
			return contract.Result{}, fmt.Errorf("test: %w", err)
		}
		one, err := t.runners.Run(ctx, resolved, target.name, args.Repeat)
		if err != nil {
			return contract.Result{}, fmt.Errorf("test: %w", err)
		}
		reports, result.FailureText = append(reports, one.Content), cmp.Or(result.FailureText, one.FailureText)
	}
	result.Content = strings.Join(reports, "\n\n")
	return result, nil
}

func (t Test) changed(ctx context.Context) ([]string, contract.Result) {
	started, inTurn := shadow.StartedIn(ctx)
	if !inTurn {
		return nil, contract.Failure("test: changed runs the tests of this turn's edits, and this call runs outside a turn: name a path instead")
	}
	timed, cancel := context.WithTimeout(ctx, konst.ShadowSnapshotTimeoutMillis*time.Millisecond)
	defer cancel()
	live, err := started.Live(timed)
	if err != nil {
		return nil, contract.Failure("test: this turn's edits could not be read from the shadow store: " + err.Error())
	}
	var paths []string
	for _, file := range live.Files {
		if resolved, err := t.root.Resolve(file.Path); err == nil && contract.IsFile(resolved) {
			paths = append(paths, file.Path)
		}
	}
	if len(paths) == 0 {
		return nil, contract.Result{Content: "test: this turn changed no file that is still on disk, so changed has no test to run"}
	}
	return paths, contract.Result{}
}
