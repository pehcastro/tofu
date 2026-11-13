package checkers

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"tofu/internal/konst"
	"tofu/internal/platform/outside"
	"tofu/internal/platform/scratch"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/shell/process"
)

type goReach int

const (
	goVetPackage goReach = iota
	goBuildTree
)

const rustHint = ": install rust from https://rustup.rs"

type NativeCheck struct {
	steps [][]string
	dir   string
	parse func(string) TestRun
}

func nativeExtensions() []string { return []string{".go", ".rs", ".py"} }

func typescriptExtensions() []string { return []string{".ts", ".tsx", ".vue", ".svelte"} }

func EditChecks(top, resolved string) ([]NativeCheck, string) {
	return nativeChecksFor(top, resolved, filepath.Ext(resolved), goVetPackage)
}

func nativeChecksFor(top, resolved, ext string, reach goReach) ([]NativeCheck, string) {
	dir := resolved
	if contract.IsFile(resolved) {
		dir = filepath.Dir(resolved)
	}
	switch ext {
	case ".go":
		module, found := contract.FindUp(dir, "go.mod")
		switch {
		case !found && reach == goVetPackage:
			return nil, ""
		case !found:
			return nil, "no go.mod in " + dir + " or above it"
		}
		pkg, _ := filepath.Rel(filepath.Dir(module), dir)
		pattern := "./" + filepath.ToSlash(pkg)
		check := NativeCheck{steps: [][]string{{"go", "vet", pattern}}, dir: filepath.Dir(module), parse: GoTest}
		if reach == goBuildTree {
			if dir == resolved {
				pattern = strings.TrimSuffix(pattern, "/.") + "/..."
			}
			check.steps = [][]string{{"go", "build", "-gcflags=-e", "-o", os.DevNull, pattern}, {"go", "vet", pattern}}
		}
		return []NativeCheck{check}, onPath("go", ": install go from https://go.dev/dl")
	case ".rs":
		manifest, found := contract.FindUp(dir, "Cargo.toml")
		if found && inCrate(filepath.Dir(manifest), resolved) {
			return []NativeCheck{{steps: [][]string{{"cargo", "check", "--all-targets"}}, dir: filepath.Dir(manifest), parse: RustProblems}}, onPath("cargo", rustHint)
		}
		files := []string{resolved}
		if dir == resolved {
			files, _ = filepath.Glob(filepath.Join(dir, "*.rs"))
		}
		if len(files) == 0 {
			return nil, "no Cargo.toml in " + dir + " or above it, and no .rs file in it"
		}
		base := cmp.Or(top, dir)
		if found && top == "" {
			base = filepath.Dir(manifest)
		}
		var checks []NativeCheck
		for _, file := range files {
			relative, _ := filepath.Rel(base, file)
			checks = append(checks, NativeCheck{steps: [][]string{{"rustc", "--crate-type", "lib", "--emit=metadata=" + os.DevNull, filepath.ToSlash(relative)}}, dir: base, parse: RustProblems})
		}
		return checks, onPath("rustc", rustHint)
	case ".py":
		project := pythonProject(top, dir)
		relative, _ := filepath.Rel(project, resolved)
		target := filepath.ToSlash(relative)
		if _, err := exec.LookPath("pyright"); err == nil {
			return []NativeCheck{{steps: [][]string{{"pyright", "--outputjson", target}}, dir: project, parse: PythonProblems}}, ""
		}
		return []NativeCheck{{steps: [][]string{{"mypy", "--show-column-numbers", target}}, dir: project, parse: PythonProblems}}, onPath("mypy", ", and neither is pyright: pip install mypy")
	}
	return nil, ""
}

func inCrate(crate, resolved string) bool {
	relative, err := filepath.Rel(crate, resolved)
	if err != nil {
		return false
	}
	first, _, _ := strings.Cut(filepath.ToSlash(relative), "/")
	return slices.Contains([]string{".", "src", "tests", "examples", "benches", "build.rs"}, first)
}

func pythonProject(top, dir string) string {
	for at := dir; ; at = filepath.Dir(at) {
		listed, _ := filepath.Glob(filepath.Join(at, "requirements*.txt"))
		if len(listed) > 0 || contract.IsFile(filepath.Join(at, "pyproject.toml")) || contract.IsFile(filepath.Join(at, "setup.cfg")) {
			return at
		}
		if at == filepath.Clean(top) || filepath.Dir(at) == at {
			return cmp.Or(top, dir)
		}
	}
}

func onPath(program, hint string) string {
	if _, err := exec.LookPath(program); err != nil {
		return program + " is not on PATH" + hint
	}
	return ""
}

func languagesUnder(resolved string) []string {
	var found []string
	visit := func(name string) bool {
		ext := filepath.Ext(name)
		if slices.Contains(typescriptExtensions(), ext) {
			ext = ".ts"
		}
		if (ext == ".ts" || slices.Contains(nativeExtensions(), ext)) && !slices.Contains(found, ext) {
			found = append(found, ext)
		}
		return len(found) <= len(nativeExtensions())
	}
	if contract.IsFile(resolved) {
		visit(resolved)
		return found
	}
	EachSourceFile(resolved, visit)
	return found
}

func (c *Typecheckers) Check(ctx context.Context, resolved string) (contract.Result, error) {
	since := time.Now()
	info, err := os.Stat(resolved)
	if err != nil {
		return contract.Result{}, err
	}
	from := resolved
	if !info.IsDir() {
		from = filepath.Dir(resolved)
	}
	languages := languagesUnder(resolved)
	_, typescript := contract.FindUp(from, TsconfigName)
	var parts []contract.Result
	if typescript && slices.Contains(languages, ".ts") || !slices.ContainsFunc(languages, func(ext string) bool { return slices.Contains(nativeExtensions(), ext) }) {
		parts = append(parts, c.checkTypescript(ctx, from, resolved, since))
	}
	for _, ext := range nativeExtensions() {
		if !slices.Contains(languages, ext) {
			continue
		}
		checks, skipped := nativeChecksFor("", resolved, ext, goBuildTree)
		if skipped != "" {
			parts = append(parts, contract.Failure("typecheck skipped: "+skipped))
			continue
		}
		text, failing := RunChecks(ctx, checks, konst.TypecheckFirstCheckMillis*time.Millisecond)
		part := contract.Result{Content: text}
		if failing {
			part.FailureText, _, _ = strings.Cut(text, "\n")
		}
		parts = append(parts, part)
	}
	merged := parts[0]
	for _, part := range parts[1:] {
		merged.Content = strings.TrimSuffix(merged.Content, "\n") + "\n" + part.Content
		merged.FailureText = cmp.Or(merged.FailureText, part.FailureText)
	}
	return merged, nil
}

func RunChecks(ctx context.Context, checks []NativeCheck, limit time.Duration) (string, bool) {
	var texts []string
	failing := false
	for _, check := range checks {
		text, failed := check.run(ctx, limit)
		texts, failing = append(texts, text), failing || failed
	}
	return strings.Join(texts, "\n"), failing
}

func (c NativeCheck) run(ctx context.Context, limit time.Duration) (string, bool) {
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	var output bytes.Buffer
	var ran []string
	var running time.Duration
	failed := false
	for _, argv := range c.steps {
		command := strings.Join(argv, " ")
		ran = append(ran, command)
		cmd := outside.Command(ctx, argv[0], argv[1:]...)
		cmd.Dir, cmd.WaitDelay, cmd.Env = c.dir, konst.BashWaitDelayMillis*time.Millisecond, checkerEnv(ctx, c.dir)
		cmd.Stdout, cmd.Stderr = &output, &output
		started := time.Now()
		tracked, err := process.StartClocked(cmd)
		if err != nil {
			return fmt.Sprintf("typecheck skipped: %s did not start: %v", command, err), true
		}
		failed = cmd.Wait() != nil
		running += time.Since(started)
		tracked.Release()
		switch {
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			return fmt.Sprintf("typecheck skipped: %s ran past its %d ms limit, so this result carries none of its errors", command, limit.Milliseconds()), true
		case ctx.Err() != nil:
			return "typecheck skipped: the turn was cancelled while " + command + " ran", true
		}
		if failed {
			break
		}
	}
	run := c.parse(output.String())
	run.Runner = fmt.Sprintf("typecheck: %s in %d ms", strings.Join(ran, " then "), running.Milliseconds())
	if failed && len(run.Problems) == 0 {
		said := strings.Split(strings.TrimSpace(output.String()), "\n")
		return run.Runner + " failed:\n" + strings.Join(said[:min(len(said), konst.TypecheckLinesCap)], "\n"), true
	}
	return run.Text(), run.errors() > 0
}

func checkerEnv(ctx context.Context, dir string) []string {
	env := append(os.Environ(), scratch.ScratchEnv(ctx)...)
	root, err := scratch.ScratchRootAt(dir)
	if err != nil {
		return env
	}
	for _, cache := range [][2]string{{"CARGO_TARGET_DIR", "cargo"}, {"MYPY_CACHE_DIR", "mypy"}} {
		if !slices.ContainsFunc(env, func(set string) bool { return strings.HasPrefix(set, cache[0]+"=") }) {
			env = append(env, cache[0]+"="+filepath.Join(root, scratch.ScratchCache, cache[1]))
		}
	}
	return env
}
