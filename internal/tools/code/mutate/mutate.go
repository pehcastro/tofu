package mutate

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"tofu/internal/models/llm/core"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/scratch/whatif"
)

const Name = "mutate"

type Limits struct {
	Check              time.Duration
	StaleOverlay       time.Duration
	TimeoutCoefficient int
	Mutants            int
}

type Mutate struct {
	root   contract.Root
	limits Limits
}

func New(dir string, limits Limits) (contract.Tool, error) {
	root, err := contract.NewRoot(dir)
	return Mutate{root: root, limits: limits}, err
}

func (Mutate) Name() string { return Name }

func (m Mutate) Definition() core.Tool {
	text := map[string]any{"type": "string"}
	return core.Tool{
		Name: Name,
		Description: "answers whether a test would catch a bug in a file: makes small bugs in the file at path one at a time, a flipped comparison, a swapped + - * /, a constant plus one, a flipped boolean, a return of the zero value, and runs the one test named test against each through whatif, never two at once, and the working tree never changes. " +
			"returns mutants, killed and every survivor as path:line with the change the test did not notice. " +
			"a go file runs go test -run ^test$ on the package at test_path, by default the file's own; a rust file runs cargo test test on the crate; a python file runs python -m unittest test, test as test_order.OrderTest.test_total; a ts or js file runs node --test --test-name-pattern ^test$. command replaces that with a program and its arguments that run that one test. " +
			fmt.Sprintf("lines, as [10, 40], keeps the mutants on those lines; past %d mutants the rest are counted and not run. a test that does not pass before mutation, or that matches no test, is refused", m.limits.Mutants),
		Parameters: map[string]any{"type": "object", "required": []string{"path", "test"}, "properties": map[string]any{
			"path":      text,
			"test":      text,
			"test_path": text,
			"command":   map[string]any{"type": "array", "items": text},
			"lines":     map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "minItems": 2, "maxItems": 2},
		}},
	}
}

type request struct {
	Path     string   `json:"path"`
	Test     string   `json:"test"`
	TestPath string   `json:"test_path"`
	Command  []string `json:"command"`
	Lines    []int    `json:"lines"`
}

type plan struct {
	mutants []mutant
	check   whatif.Check
	ranTest *regexp.Regexp
}

func (m Mutate) Run(ctx context.Context, raw json.RawMessage) (contract.Result, error) {
	var req request
	if err := json.Unmarshal(raw, &req); err != nil {
		return contract.Result{}, fmt.Errorf("mutate: arguments are not the expected shape: %w", err)
	}
	if req.Path == "" || req.Test == "" {
		return contract.Failure("mutate: path and test are required: the file to put bugs in and the one test that should catch them"), nil
	}
	resolved, err := m.root.Resolve(req.Path)
	if err != nil {
		return contract.Failure("mutate: path " + err.Error()), nil
	}
	read, err := os.ReadFile(resolved)
	if err != nil {
		return contract.Failure("mutate: " + err.Error()), nil
	}
	body := string(read)
	rel, _ := filepath.Rel(string(m.root), resolved)
	rel = filepath.ToSlash(rel)
	planned, err := m.planned(req, rel, body)
	if err != nil {
		return contract.Failure("mutate: " + err.Error()), nil
	}
	mutants, check := planned.mutants, planned.check
	if len(req.Lines) == 2 {
		mutants = slices.DeleteFunc(mutants, func(m mutant) bool { return m.line < req.Lines[0] || m.line > req.Lines[1] })
	}
	if len(mutants) == 0 {
		return contract.Failure("mutate: " + rel + " has nothing to change on those lines: no comparison, arithmetic, constant, boolean or return"), nil
	}
	limits := whatif.Limits{Check: m.limits.Check, StaleOverlay: m.limits.StaleOverlay}
	started := time.Now()
	baseline, err := whatif.Try(ctx, m.root, nil, check, limits)
	if err != nil {
		return contract.Failure("mutate: " + err.Error()), nil
	}
	if baseline.Outcome != whatif.Passed {
		return contract.Failure("mutate: the test does not pass before mutation, so no mutant can say anything about it: " + baseline.Outcome.String() + "\n" + baseline.Digest), nil
	}
	if planned.ranTest != nil && !planned.ranTest.MatchString(baseline.Digest) {
		return contract.Failure("mutate: " + req.Test + " ran no test before mutation, so every mutant would survive it\n" + baseline.Digest), nil
	}
	took := time.Since(started)
	limits.Check = min(m.limits.Check, took*time.Duration(m.limits.TimeoutCoefficient))
	ran := mutants[:min(len(mutants), m.limits.Mutants)]
	var survived []string
	killed, timedOut := 0, 0
	for _, mutant := range ran {
		answer, err := whatif.Try(ctx, m.root, map[string]string{rel: mutant.applied(body)}, check, limits)
		if err != nil {
			return contract.Result{}, fmt.Errorf("mutate: %w", err)
		}
		switch answer.Outcome {
		case whatif.Passed:
			survived = append(survived, fmt.Sprintf("%s:%d: %s", rel, mutant.line, mutant.change(body)))
		case whatif.Failed:
			killed++
		case whatif.TimedOut:
			killed, timedOut = killed+1, timedOut+1
		case whatif.NotRun, whatif.NoTests:
			return contract.Failure(fmt.Sprintf("mutate: the mutant at %s:%d did not test: %s\n%s", rel, mutant.line, answer.Outcome, answer.Digest)), nil
		}
	}
	var out strings.Builder
	fmt.Fprintf(&out, "mutants: %d, killed: %d, survived: %d\n", len(ran), killed, len(survived))
	if timedOut > 0 {
		fmt.Fprintf(&out, "%d killed by running past %d ms\n", timedOut, limits.Check.Milliseconds())
	}
	if len(survived) > 0 {
		out.WriteString("survived:\n" + strings.Join(survived, "\n") + "\n")
	}
	if left := len(mutants) - len(ran); left > 0 {
		fmt.Fprintf(&out, "%d more mutants were not run: give lines to choose them\n", left)
	}
	fmt.Fprintf(&out, "test: check %s %s in %s passed before mutation in %d ms; each mutant ran alone, limited to %d ms; the working tree was not changed", check.Kind, cmp.Or(check.Name, strings.Join(check.Command, " ")), check.Path, took.Milliseconds(), limits.Check.Milliseconds())
	return contract.Result{Content: out.String()}, nil
}

func (m Mutate) planned(req request, rel, body string) (plan, error) {
	command := whatif.Check{Kind: whatif.Command, Path: cmp.Or(req.TestPath, ".")}
	tested := whatif.Check{Kind: whatif.Test, Path: cmp.Or(req.TestPath, filepath.Dir(rel)), Name: req.Test}
	anchored := "^" + regexp.QuoteMeta(req.Test) + "$"
	operators := ` (===|!==|==|!=|<=|>=|&&|\|\||<|>|\+|-|\*|/) `
	var p plan
	var err error
	switch filepath.Ext(rel) {
	case ".go":
		p.mutants, err = goMutants(body)
		tested.Name = anchored
		p.check = tested
	case ".rs":
		p.mutants = textMutants(body, syntax{lineComment: "//", blockComment: true, quotes: `"`, operators: regexp.MustCompile(operators), booleans: regexp.MustCompile(`\b(true|false)\b`), zero: "Default::default()", testsFrom: "#[cfg(test)]"})
		p.check = tested
	case ".py":
		p.mutants = textMutants(body, syntax{lineComment: "#", quotes: `"'`, operators: regexp.MustCompile(` (==|!=|<=|>=|<|>|\+|-|\*|/|and|or) `), booleans: regexp.MustCompile(`\b(True|False)\b`), zero: "None"})
		command.Command = []string{m.python(), "-B", "-m", "unittest", req.Test}
		p.check, p.ranTest = command, regexp.MustCompile(`Ran [1-9]\d* tests? in`)
	case ".ts", ".tsx", ".mts", ".cts", ".js", ".mjs", ".cjs":
		p.mutants = textMutants(body, syntax{lineComment: "//", blockComment: true, quotes: "\"'`", operators: regexp.MustCompile(operators), booleans: regexp.MustCompile(`\b(true|false)\b`), zero: "undefined"})
		command.Command = []string{"node", "--test", "--test-name-pattern=" + anchored}
		p.check, p.ranTest = command, regexp.MustCompile(`(?m)^\s*(?:\x{2714}|ok \d+ -) `+regexp.QuoteMeta(req.Test)+`(?: \(|$)`)
	default:
		return plan{}, fmt.Errorf("%s is not a go, rust, python, ts or js file, so there is no mutator for it", rel)
	}
	if len(req.Command) > 0 {
		command.Command = req.Command
		p.check, p.ranTest = command, nil
	}
	return p, err
}

func (m Mutate) python() string {
	for _, venv := range []string{filepath.Join(".venv", "Scripts", "python.exe"), filepath.Join(".venv", "bin", "python"), filepath.Join("venv", "Scripts", "python.exe"), filepath.Join("venv", "bin", "python")} {
		if contract.IsFile(filepath.Join(string(m.root), venv)) {
			return filepath.Join(string(m.root), venv)
		}
	}
	for _, name := range []string{"python", "python3"} {
		if _, err := exec.LookPath(name); err == nil {
			return name
		}
	}
	return "python3"
}
