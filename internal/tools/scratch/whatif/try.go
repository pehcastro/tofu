package whatif

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/internal/konst"
	"tofu/internal/platform/outside"
	"tofu/internal/platform/scratch"
	"tofu/internal/tools/code/typecheck/checkers"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/scratch/overlay"
	"tofu/internal/tools/shell/process"
)

type CheckKind string

const (
	Build   CheckKind = "build"
	Test    CheckKind = "test"
	Command CheckKind = "command"
)

type Check struct {
	Kind    CheckKind `json:"check"`
	Path    string    `json:"path"`
	Name    string    `json:"name"`
	Command []string  `json:"command"`
}

type Outcome int

const (
	Passed Outcome = iota
	Failed
	TimedOut
	NotRun
	NoTests
)

func (o Outcome) String() string {
	switch o {
	case Passed:
		return "passed"
	case Failed:
		return "the check failed"
	case TimedOut:
		return "the check timed out"
	case NotRun:
		return "the check did not run"
	case NoTests:
		return "no test ran, so it says nothing about the edits"
	}
	return "outcome " + strconv.Itoa(int(o))
}

type Answer struct {
	Outcome Outcome
	Digest  string
	Hash    string
}

type Limits struct {
	Check        time.Duration
	StaleOverlay time.Duration
}

type plan struct {
	steps [][]string
	dir   string
	parse func(string) checkers.TestRun
	tests bool
}

func Try(ctx context.Context, root contract.Root, files map[string]string, check Check, limits Limits, hidden ...string) (Answer, error) {
	from, err := root.Resolve(cmp.Or(check.Path, "."))
	if err != nil {
		return Answer{}, fmt.Errorf("whatif: %w", err)
	}
	made, err := overlay.Make(string(root), files, limits.StaleOverlay, hidden...)
	if err != nil {
		return Answer{}, fmt.Errorf("whatif: the overlay was not made: %w", err)
	}
	defer func() { _ = made.Remove() }()
	planned, err := planOf(made, files, string(root), from, check)
	if err != nil {
		return Answer{}, err
	}
	return planned.run(ctx, made, limits.Check), nil
}

func planOf(made overlay.Overlay, files map[string]string, root, from string, check Check) (plan, error) {
	inTree := func(dir string) (string, error) {
		tree, err := made.Tree()
		rel, _ := filepath.Rel(root, dir)
		return filepath.Join(tree, rel), err
	}
	switch check.Kind {
	case Command:
		if len(check.Command) == 0 {
			return plan{}, errors.New("whatif: check command needs command, the program and its arguments as a list")
		}
		dir, err := inTree(from)
		return plan{steps: [][]string{check.Command}, dir: dir, parse: unparsed}, err
	case Build, Test:
	default:
		return plan{}, fmt.Errorf("whatif: check %q is none of build, test or command", check.Kind)
	}
	if contract.IsFile(from) || filepath.Ext(from) == ".go" {
		from = filepath.Dir(from)
	}
	project, at := projectOf(root, from)
	switch project {
	case goModule:
		rel, _ := filepath.Rel(at, from)
		pkg := "./" + filepath.ToSlash(rel)
		dir, overlaid, err := at, []string{made.GoFlag()}, error(nil)
		_, absent := os.Stat(from)
		for edited := range files {
			if _, missing := os.Stat(filepath.Join(root, filepath.Dir(filepath.FromSlash(edited)))); missing != nil {
				absent = missing
			}
		}
		if absent != nil {
			dir, err = inTree(at)
			overlaid = nil
		}
		if check.Kind == Build {
			return plan{steps: [][]string{slices.Concat([]string{"go", "build"}, overlaid, []string{"-o", os.DevNull, pkg}), slices.Concat([]string{"go", "vet"}, overlaid, []string{pkg})}, dir: dir, parse: checkers.GoTest}, err
		}
		argv := slices.Concat([]string{"go", "test"}, overlaid, []string{"-json", "-count=1"})
		if check.Name != "" {
			argv = append(argv, "-run", check.Name)
		}
		return plan{steps: [][]string{append(argv, pkg)}, dir: dir, parse: checkers.GoTest, tests: true}, err
	case rustCrate:
		dir, err := inTree(at)
		if check.Kind == Build {
			return plan{steps: [][]string{{"cargo", "build", "--color", "never"}}, dir: dir, parse: checkers.RustProblems}, err
		}
		argv := []string{"cargo", "test", "--color", "never"}
		if check.Name != "" {
			argv = append(argv, check.Name)
		}
		return plan{steps: [][]string{argv}, dir: dir, parse: checkers.CargoTest, tests: true}, err
	case otherProject:
		return plan{}, fmt.Errorf("whatif: build and test run on a go module or a rust crate, and no go.mod or Cargo.toml is at %s or above it inside the project: give check command with the command that checks it", from)
	}
	return plan{}, fmt.Errorf("whatif: no check for project kind %d", project)
}

type projectKind int

const (
	goModule projectKind = iota
	rustCrate
	otherProject
)

func projectOf(root, from string) (projectKind, string) {
	for at := from; ; at = filepath.Dir(at) {
		switch {
		case contract.IsFile(filepath.Join(at, "go.mod")):
			return goModule, at
		case contract.IsFile(filepath.Join(at, "Cargo.toml")):
			return rustCrate, at
		case at == root || filepath.Dir(at) == at:
			return otherProject, root
		}
	}
}

func (p plan) run(ctx context.Context, made overlay.Overlay, limit time.Duration) Answer {
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	var output bytes.Buffer
	var ran []string
	started, outcome, exit := time.Now(), Passed, 0
	for _, argv := range p.steps {
		command := strings.Join(argv, " ")
		ran = append(ran, command)
		cmd := outside.Command(ctx, argv[0], argv[1:]...)
		cmd.Dir, cmd.WaitDelay = p.dir, konst.BashWaitDelayMillis*time.Millisecond
		cmd.Env = slices.Concat(os.Environ(), []string{"GOMAXPROCS=" + strconv.Itoa(konst.TestCPUs)}, scratch.ScratchEnv(ctx))
		cmd.Stdout, cmd.Stderr = &output, &output
		tracked, err := process.StartClocked(cmd)
		if err != nil {
			return answered(NotRun, "", fmt.Sprintf("%s did not start in %s: %v", command, p.dir, err))
		}
		_ = cmd.Wait()
		tracked.Release()
		switch {
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			return answered(TimedOut, "", fmt.Sprintf("%s ran past its %d ms limit and was stopped, so it says nothing about the edits", command, limit.Milliseconds()))
		case ctx.Err() != nil:
			return answered(NotRun, "", "the call was cancelled while "+command+" ran")
		}
		if exit = cmd.ProcessState.ExitCode(); exit != 0 {
			outcome = Failed
			break
		}
	}
	head := fmt.Sprintf("%s in %s, %d ms", strings.Join(ran, " then "), p.dir, time.Since(started).Milliseconds())
	printed := made.Local(strings.TrimSpace(strings.ReplaceAll(output.String(), "\r\n", "\n")), p.dir)
	parsed := p.parse(printed)
	explained := parsed.Explains(exit)
	if p.tests && outcome == Passed && (!explained || parsed.Passed == 0) {
		outcome = NoTests
	}
	switch {
	case explained:
		return answered(outcome, head, strings.TrimSuffix(parsed.Text(), "\n"))
	case printed == "":
		return answered(outcome, head, ", printed nothing")
	}
	lines := strings.Split(printed, "\n")
	return answered(outcome, head, ", its last lines:\n"+strings.Join(lines[max(0, len(lines)-konst.TestLinesCap):], "\n"))
}

func answered(outcome Outcome, head, said string) Answer {
	sum := sha256.Sum256([]byte(said))
	return Answer{Outcome: outcome, Digest: head + said, Hash: hex.EncodeToString(sum[:])}
}

func unparsed(string) checkers.TestRun { return checkers.TestRun{} }
