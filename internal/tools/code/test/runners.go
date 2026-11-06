package test

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/internal/konst"
	"tofu/internal/platform/scratch"
	"tofu/internal/tools/code/typecheck/checkers"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/shell/process"
)

type language int

const (
	goModule language = iota
	rustCrate
	pythonProject
	nodePackage
)

type testOutcome int

const (
	testsPassed testOutcome = iota
	testsFailed
	testsNotRun
)

type testCommand struct {
	runner           string
	argv             []string
	dir              string
	env              []string
	install          string
	notInstalled     string
	notInstalledSays string
	parse            func(string) checkers.TestRun
	skips            func(string) []string
}

func (r *TestRunners) Run(ctx context.Context, resolved, name string, repeat int) (contract.Result, error) {
	info, err := os.Stat(resolved)
	if err != nil {
		return contract.Result{}, err
	}
	from := resolved
	if !info.IsDir() {
		from = filepath.Dir(resolved)
	}
	found, root, ok := projectOf(from)
	if !ok {
		return contract.Failure("test skipped: no go.mod, Cargo.toml, python project file or package.json in " + from + " or above it, so run this project's tests through the shell"), nil
	}
	var command testCommand
	var none string
	switch found {
	case nodePackage:
		if name != "" || repeat > 1 {
			return contract.Failure("test: name and repeat run go, rust and python tests; vitest runs whole files, so name the test file as path and call again to repeat it"), nil
		}
		return r.Test(ctx, resolved)
	case goModule:
		command, none = goTests(root, resolved, info.IsDir(), name)
	case rustCrate:
		command, none = rustTests(root, resolved, info.IsDir(), name)
	case pythonProject:
		command, none = pythonTests(root, resolved, info.IsDir(), name)
	default:
		return contract.Result{}, fmt.Errorf("no test runner for language %d", found)
	}
	if none != "" {
		return contract.Result{Content: none}, nil
	}
	if repeat > 1 {
		return command.repeated(ctx, repeat), nil
	}
	result, _ := command.run(ctx)
	return result, nil
}

func projectOf(from string) (language, string, bool) {
	markers := []struct {
		language language
		pattern  string
	}{
		{goModule, "go.mod"}, {rustCrate, "Cargo.toml"},
		{pythonProject, "pyproject.toml"}, {pythonProject, "setup.py"}, {pythonProject, "setup.cfg"},
		{pythonProject, "pytest.ini"}, {pythonProject, "tox.ini"}, {pythonProject, "requirements*.txt"},
		{nodePackage, "package.json"},
	}
	for at := contract.Absolute(from); ; at = filepath.Dir(at) {
		entries, _ := os.ReadDir(at)
		for _, marker := range markers {
			if slices.ContainsFunc(entries, func(entry os.DirEntry) bool {
				matched, _ := filepath.Match(marker.pattern, entry.Name())
				return matched && !entry.IsDir()
			}) {
				return marker.language, at, true
			}
		}
		if filepath.Dir(at) == at {
			return goModule, "", false
		}
	}
}

func goTests(root, resolved string, isDir bool, name string) (testCommand, string) {
	dir := resolved
	if !isDir {
		dir = filepath.Dir(resolved)
	}
	pkg, _ := filepath.Rel(root, dir)
	pkg = filepath.ToSlash(pkg)
	var selected string
	switch {
	case name != "":
		selected = "^" + name + "$"
	case isDir:
	case filepath.Ext(resolved) != ".go":
		return testCommand{}, "test: no test for " + filepath.Base(resolved) + ": it is not a .go file"
	default:
		file := resolved
		if !strings.HasSuffix(file, "_test.go") {
			file = strings.TrimSuffix(file, ".go") + "_test.go"
		}
		body, _ := os.ReadFile(file)
		var funcs []string
		for _, match := range regexp.MustCompile(`(?m)^func (Test\w*)\(\w+ \*testing\.T\)`).FindAllSubmatch(body, -1) {
			funcs = append(funcs, string(match[1]))
		}
		if len(funcs) == 0 {
			return testCommand{}, "test: no test for " + filepath.Base(resolved) + ": looked for func Test in " + filepath.Base(file) + " beside it"
		}
		selected = "^(" + strings.Join(funcs, "|") + ")$"
	}
	argv := []string{"go", "test", "-json", "-count=1"}
	if selected != "" {
		argv = append(argv, "-run", selected)
	}
	target := "./" + pkg
	if pkg == "." {
		target = pkg
	}
	return testCommand{
		runner:  "go test",
		argv:    append(argv, target),
		dir:     root,
		env:     []string{"GOMAXPROCS=" + strconv.Itoa(konst.TestCPUs)},
		install: "install Go from https://go.dev/dl and put its bin directory on PATH",
		parse: func(output string) checkers.TestRun {
			run := checkers.GoTest(output)
			for i, failure := range run.Failures {
				if failure.At != "" && pkg != "." {
					run.Failures[i].At = pkg + "/" + failure.At
				}
			}
			return run
		},
		skips: func(output string) []string {
			var skipped []string
			for _, line := range strings.Split(output, "\n") {
				var event struct{ Action, Test string }
				if json.Unmarshal([]byte(line), &event) == nil && event.Action == "skip" && event.Test != "" {
					skipped = append(skipped, event.Test)
				}
			}
			return skipped
		},
	}, ""
}

func rustTests(root, resolved string, isDir bool, name string) (testCommand, string) {
	jobs := strconv.Itoa(konst.TestCPUs)
	argv := []string{"cargo", "test", "--color", "never", "--jobs", jobs}
	relative, _ := filepath.Rel(root, resolved)
	relative = filepath.ToSlash(relative)
	module := strings.TrimSuffix(strings.TrimSuffix(strings.TrimPrefix(relative, "src/"), ".rs"), "/mod")
	switch {
	case name != "":
		argv = append(argv, name)
	case isDir:
	case filepath.Ext(resolved) != ".rs":
		return testCommand{}, "test: no test for " + filepath.Base(resolved) + ": it is not a .rs file"
	case path.Dir(relative) == "tests":
		argv = append(argv, "--test", strings.TrimSuffix(path.Base(relative), ".rs"))
	case strings.HasPrefix(relative, "src/") && module != "lib" && module != "main":
		argv = append(argv, strings.ReplaceAll(module, "/", "::")+"::")
	}
	return testCommand{
		runner:  "cargo test",
		argv:    append(argv, "--", "--test-threads", jobs),
		dir:     root,
		install: "install Rust with rustup from https://rustup.rs and put its cargo bin directory on PATH",
		parse:   checkers.CargoTest,
		skips:   namesMatching(`(?m)^test (\S+) \.\.\. ignored`),
	}, ""
}

func pythonTests(root, resolved string, isDir bool, name string) (testCommand, string) {
	target := resolved
	if base := filepath.Base(resolved); !isDir {
		stem, isPython := strings.CutSuffix(base, ".py")
		if !isPython {
			return testCommand{}, "test: no test for " + base + ": it is not a .py file"
		}
		if !strings.HasPrefix(stem, "test_") && !strings.HasSuffix(stem, "_test") {
			candidates := []string{filepath.Join(filepath.Dir(resolved), "test_"+base), filepath.Join(filepath.Dir(resolved), stem+"_test.py"), filepath.Join(root, "tests", "test_"+base)}
			at := slices.IndexFunc(candidates, contract.IsFile)
			if at < 0 {
				return testCommand{}, "test: no test for " + base + ": looked for test_" + base + " and " + stem + "_test.py beside it and tests/test_" + base
			}
			target = candidates[at]
		}
	}
	relative, _ := filepath.Rel(root, target)
	relative = filepath.ToSlash(relative)
	python := pythonIn(root)
	command := testCommand{dir: root, install: "install Python 3 from https://www.python.org/downloads, or make a virtual environment at " + filepath.Join(root, ".venv")}
	if pytestDeclared(root) {
		command.argv = []string{python, "-m", "pytest", "-rA"}
		if name != "" {
			command.argv = append(command.argv, "-k", name)
		}
		command.argv = append(command.argv, relative)
		command.runner, command.parse = "pytest", checkers.Pytest
		command.notInstalled, command.notInstalledSays = "No module named pytest", root+" declares pytest and "+python+" cannot import it: run "+python+" -m pip install pytest"
		command.skips = namesMatching(`(?m)^SKIPPED \[\d+\] (\S+?:\d+)`)
		return command, ""
	}
	argv := []string{python, "-m", "unittest"}
	if isDir {
		argv = append(argv, "discover", "-s", relative, "-t", ".")
	}
	argv = append(argv, "-v")
	if name != "" {
		argv = append(argv, "-k", name)
	}
	if !isDir {
		argv = append(argv, strings.ReplaceAll(strings.TrimSuffix(relative, ".py"), "/", "."))
	}
	command.runner, command.argv, command.parse = "unittest", argv, checkers.Unittest
	command.skips = namesMatching(`(?m)^(\w+ \(\S+\)) \.\.\. skipped`)
	return command, ""
}

func pythonIn(root string) string {
	for _, venv := range []string{filepath.Join(root, ".venv", "Scripts", "python.exe"), filepath.Join(root, ".venv", "bin", "python"), filepath.Join(root, "venv", "Scripts", "python.exe"), filepath.Join(root, "venv", "bin", "python")} {
		if contract.IsFile(venv) {
			return venv
		}
	}
	for _, name := range []string{"python", "python3"} {
		if _, err := exec.LookPath(name); err == nil {
			return name
		}
	}
	return "python3"
}

func pytestDeclared(root string) bool {
	declaring, _ := filepath.Glob(filepath.Join(root, "requirements*.txt"))
	for _, name := range []string{"pytest.ini", "pyproject.toml", "setup.cfg", "tox.ini"} {
		declaring = append(declaring, filepath.Join(root, name))
	}
	return contract.IsFile(filepath.Join(root, "conftest.py")) || slices.ContainsFunc(declaring, func(file string) bool {
		body, err := os.ReadFile(file)
		return err == nil && bytes.Contains(body, []byte("pytest"))
	})
}

func namesMatching(pattern string) func(string) []string {
	return func(output string) []string {
		var names []string
		for _, match := range regexp.MustCompile(pattern).FindAllStringSubmatch(output, -1) {
			names = append(names, match[1])
		}
		return names
	}
}

func (c testCommand) run(ctx context.Context) (contract.Result, testOutcome) {
	command := strings.Join(c.argv, " ")
	if _, err := exec.LookPath(c.argv[0]); err != nil {
		return contract.Failure(fmt.Sprintf("test skipped: %s is not on PATH, so %s cannot run in %s: %s", c.argv[0], command, c.dir, c.install)), testsNotRun
	}
	ctx, cancel := context.WithTimeout(ctx, konst.TestRunDeadlineMillis*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.argv[0], c.argv[1:]...)
	cmd.Dir, cmd.WaitDelay, cmd.Env = c.dir, konst.BashWaitDelayMillis*time.Millisecond, slices.Concat(os.Environ(), c.env, scratch.ScratchEnv(ctx))
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	started := time.Now()
	tracked, err := process.StartTracked(cmd)
	if err != nil {
		return contract.Failure(fmt.Sprintf("test skipped: %s did not start in %s: %v", command, c.dir, err)), testsNotRun
	}
	defer tracked.Release()
	_ = cmd.Wait()
	head := fmt.Sprintf("test: %s in %s, %d ms", command, c.dir, time.Since(started).Milliseconds())
	printed, exit := strings.ReplaceAll(output.String(), "\r\n", "\n"), cmd.ProcessState.ExitCode()
	run := c.parse(printed)
	run.Runner = c.runner
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return contract.Failure(fmt.Sprintf("%s: ran past its %d ms limit and was stopped", head, konst.TestRunDeadlineMillis)), testsNotRun
	case ctx.Err() != nil:
		return contract.Failure("test skipped: the turn was cancelled while " + command + " ran"), testsNotRun
	case c.notInstalled != "" && strings.Contains(printed, c.notInstalled):
		return contract.Failure("test skipped: " + c.notInstalledSays), testsNotRun
	case run.Passed+run.Failed+run.Skipped == 0 && len(run.Failures) == 0 && len(run.Problems) == 0 && (exit == 0 || run.Listed):
		return contract.Result{Content: head + ": 0 tests found"}, testsNotRun
	case !run.Explains(exit):
		lines := strings.Split(strings.TrimSpace(printed), "\n")
		return contract.Failure(fmt.Sprintf("%s: exited %d and printed no test run tofu reads. its last lines:\n%s", head, exit, strings.Join(lines[max(0, len(lines)-konst.TestLinesCap):], "\n"))), testsNotRun
	}
	report := head + "\n" + strings.TrimSuffix(run.Text(), "\n")
	if skipped := c.skips(printed); len(skipped) > 0 {
		report += "\nskipped: " + strings.Join(skipped, ", ")
	}
	if exit != 0 {
		return contract.Failure(report), testsFailed
	}
	return contract.Result{Content: report}, testsPassed
}

func (c testCommand) repeated(ctx context.Context, times int) contract.Result {
	ctx, cancel := context.WithTimeout(ctx, konst.TestRunDeadlineMillis*time.Millisecond)
	defer cancel()
	passed, failed, firstFailure, stopped := 0, 0, "", ""
	for range times {
		if stopped != "" {
			break
		}
		result, outcome := c.run(ctx)
		switch outcome {
		case testsPassed:
			passed++
		case testsFailed:
			failed++
			firstFailure = cmp.Or(firstFailure, result.Content)
		case testsNotRun:
			if passed+failed == 0 {
				return result
			}
			first, _, _ := strings.Cut(result.Content, "\n")
			stopped = ", then stopped: " + first
		}
	}
	verdict := "flaky"
	switch {
	case failed == 0:
		verdict = "passes"
	case passed == 0:
		verdict = "real"
	}
	head := fmt.Sprintf("test: repeated %s %d of %d times%s: %d passed, %d failed, verdict %s", strings.Join(c.argv, " "), passed+failed, times, stopped, passed, failed, verdict)
	if failed == 0 {
		return contract.Result{Content: head}
	}
	return contract.Failure(head + "\nthe first failing run:\n" + firstFailure)
}
