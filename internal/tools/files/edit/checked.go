package edit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"tofu/internal/konst"
	"tofu/internal/platform/scratch"
	"tofu/internal/tools/code/typecheck/checkers"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/shell/process"
)

type afterCheck struct {
	argv  []string
	dir   string
	parse func(string) checkers.TestRun
}

func Checked(ctx context.Context, typecheckers *checkers.Typecheckers, root contract.Root, resolved, result string) string {
	check, skipped := afterCheckFor(root, resolved)
	switch {
	case skipped != "":
		return result + "\n\ntypecheck skipped: " + skipped
	case check.argv == nil:
		return typecheckers.Typechecked(ctx, resolved, result)
	}
	return result + "\n\n" + check.run(ctx)
}

func afterCheckFor(root contract.Root, resolved string) (afterCheck, string) {
	dir := filepath.Dir(resolved)
	switch filepath.Ext(resolved) {
	case ".go":
		module, found := contract.FindUp(dir, "go.mod")
		if !found {
			return afterCheck{}, ""
		}
		pkg, _ := filepath.Rel(filepath.Dir(module), dir)
		return afterCheck{argv: []string{"go", "vet", "./" + filepath.ToSlash(pkg)}, dir: filepath.Dir(module), parse: checkers.GoTest}, onPath("go", ": install go from https://go.dev/dl")
	case ".rs":
		manifest, found := contract.FindUp(dir, "Cargo.toml")
		if !found {
			return afterCheck{}, "no Cargo.toml in " + dir + " or above it"
		}
		return afterCheck{argv: []string{"cargo", "check"}, dir: filepath.Dir(manifest), parse: checkers.RustProblems}, onPath("cargo", ": install rust from https://rustup.rs")
	case ".py":
		project := pythonProject(root, dir)
		relative, _ := filepath.Rel(project, resolved)
		file := filepath.ToSlash(relative)
		if _, err := exec.LookPath("pyright"); err == nil {
			return afterCheck{argv: []string{"pyright", "--outputjson", file}, dir: project, parse: checkers.PythonProblems}, ""
		}
		return afterCheck{argv: []string{"mypy", "--show-column-numbers", file}, dir: project, parse: checkers.PythonProblems}, onPath("mypy", ", and neither is pyright: pip install mypy")
	}
	return afterCheck{}, ""
}

func pythonProject(root contract.Root, dir string) string {
	for at := dir; ; at = filepath.Dir(at) {
		listed, _ := filepath.Glob(filepath.Join(at, "requirements*.txt"))
		if len(listed) > 0 || contract.IsFile(filepath.Join(at, "pyproject.toml")) || contract.IsFile(filepath.Join(at, "setup.cfg")) {
			return at
		}
		if at == filepath.Clean(string(root)) || filepath.Dir(at) == at {
			return string(root)
		}
	}
}

func onPath(program, hint string) string {
	if _, err := exec.LookPath(program); err != nil {
		return program + " is not on PATH" + hint
	}
	return ""
}

func (c afterCheck) run(ctx context.Context) string {
	command := strings.Join(c.argv, " ")
	ctx, cancel := context.WithTimeout(ctx, konst.TypecheckInlineFirstMillis*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.argv[0], c.argv[1:]...)
	cmd.Dir, cmd.WaitDelay, cmd.Env = c.dir, konst.BashWaitDelayMillis*time.Millisecond, append(os.Environ(), scratch.ScratchEnv(ctx)...)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	started := time.Now()
	tracked, err := process.StartTracked(cmd)
	if err != nil {
		return fmt.Sprintf("typecheck skipped: %s did not start: %v", command, err)
	}
	defer tracked.Release()
	failed := cmd.Wait() != nil
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return fmt.Sprintf("typecheck skipped: %s ran past its %d ms limit after this change, so this result carries none of its errors", command, konst.TypecheckInlineFirstMillis)
	case ctx.Err() != nil:
		return "typecheck skipped: the turn was cancelled while " + command + " ran"
	}
	run := c.parse(output.String())
	run.Runner = fmt.Sprintf("typecheck: %s in %d ms", command, time.Since(started).Milliseconds())
	if failed && len(run.Problems) == 0 {
		said := strings.Split(strings.TrimSpace(output.String()), "\n")
		return run.Runner + " failed:\n" + strings.Join(said[:min(len(said), konst.TypecheckLinesCap)], "\n")
	}
	return run.Text()
}
