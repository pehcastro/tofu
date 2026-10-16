package main

import (
	"bufio"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"tofu/internal/guard"
	"tofu/internal/sys"
)

const (
	childProcs   = "4"
	testParallel = "2"
	module       = "tofu"
	benchPrefix  = module + "/bench/"
)

var diagnostic = sys.LazyRegexp(`^(.+\.go):\d+:\d+: `)

var roots = []string{"./cmd/...", "./internal/...", "./interface/...", "./library/...", "./bench/..."}

type failure struct {
	step, pkg, detail string
	tests             []string
	flaky             bool
}

func (f failure) blocks() bool { return !f.flaky && !strings.HasPrefix(f.pkg, benchPrefix) }

func (f failure) result() string {
	switch {
	case f.flaky:
		return "flaky, passed alone"
	case f.blocks():
		return "FAIL, blocks"
	}
	return "fail, bench, apart"
}

type runner struct{ repo, bash string }

func main() {
	stamped, err := run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "treewide:", err)
		os.Exit(2)
	}
	if !stamped {
		os.Exit(1)
	}
}

func run() (bool, error) {
	top, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return false, fmt.Errorf("not inside a git repository: %w", err)
	}
	r := runner{repo: strings.TrimSpace(string(top))}
	if r.bash, err = exec.LookPath("bash"); err != nil {
		return false, err
	}
	if runtime.GOOS == "windows" && strings.Contains(strings.ToLower(r.bash), `\system32\`) {
		return false, fmt.Errorf("bash resolves to the WSL launcher at %s; put Git Bash first on PATH", r.bash)
	}
	tree, dirty, err := guard.HeadTree(r.repo)
	if err != nil {
		return false, err
	}
	if len(dirty) > 0 {
		fmt.Printf("no stamp will be written: %d uncommitted Go files, and a stamp is keyed by HEAD's tree; run in a clean clone of HEAD\n", len(dirty))
	}
	head, _ := exec.Command("git", "-C", r.repo, "rev-parse", "--short", "HEAD").Output()
	machine, _ := os.Hostname()
	fmt.Printf("treewide: tree %s, HEAD %s, %s %s/%s %d cpu, GOMAXPROCS %s, -p %s, bash %s, %s\n",
		tree, strings.TrimSpace(string(head)), machine, runtime.GOOS, runtime.GOARCH, runtime.NumCPU(), childProcs, testParallel, r.bash, time.Now().Format(time.RFC3339))
	started := time.Now()
	failures, err := r.vet()
	if err != nil {
		return false, err
	}
	tested, passed, err := r.test()
	if err != nil {
		return false, err
	}
	failures = append(failures, tested...)
	fmt.Println("\n| step | package | result | detail |\n|---|---|---|---|")
	for _, f := range failures {
		fmt.Printf("| %s | %s | %s | %s |\n", f.step, f.pkg, f.result(), strings.TrimSpace(strings.Join(append(f.tests, f.detail), " ")))
	}
	blocking := slices.ContainsFunc(failures, failure.blocks)
	fmt.Printf("\n%d packages passed, %d rows above, in %s\n", passed, len(failures), time.Since(started).Round(time.Second))
	if blocking {
		fmt.Println("no stamp: a product package failed")
		return false, nil
	}
	if len(dirty) > 0 {
		return false, nil
	}
	return stamp(r.repo, tree, strings.TrimSpace(string(head)), passed, failures)
}

func (r runner) goCommand(args ...string) *exec.Cmd {
	cmd := exec.Command(r.bash, append([]string{"-c", `exec "$0" "$@"`, "go"}, args...)...)
	cmd.Dir, cmd.Env = r.repo, append(os.Environ(), "GOMAXPROCS="+childProcs)
	return cmd
}

func (r runner) vet() ([]failure, error) {
	out, err := r.goCommand(append([]string{"vet", "-p", testParallel}, roots...)...).CombinedOutput()
	if err == nil {
		fmt.Println("vet: clean")
		return nil, nil
	}
	if !errors.As(err, new(*exec.ExitError)) {
		return nil, err
	}
	var failures []failure
	for _, line := range strings.Split(string(out), "\n") {
		found := diagnostic().FindStringSubmatch(strings.TrimSpace(line))
		if found == nil {
			continue
		}
		file := found[1]
		if filepath.IsAbs(file) {
			file, _ = filepath.Rel(r.repo, file)
		}
		pkg := module + "/" + filepath.ToSlash(filepath.Dir(file))
		if !slices.ContainsFunc(failures, func(f failure) bool { return f.pkg == pkg }) {
			failures = append(failures, failure{step: "vet", pkg: pkg, detail: strings.TrimSpace(line)})
		}
	}
	if len(failures) == 0 {
		failures = []failure{{step: "vet", pkg: "unknown", detail: lastLine(string(out))}}
	}
	fmt.Printf("vet: %d packages\n", len(failures))
	return failures, nil
}

type testEvent struct {
	Action, Package, ImportPath, Test, Output string
	Elapsed                                   float64
}

func (r runner) test() ([]failure, int, error) {
	cmd := r.goCommand(append([]string{"test", "-p", testParallel, "-count=1", "-json"}, roots...)...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, 0, err
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, 0, err
	}
	failed, outputs, passed := map[string][]string{}, map[string]string{}, 0
	var order []string
	lines := bufio.NewScanner(stdout)
	lines.Buffer(make([]byte, 1<<20), 1<<24)
	for lines.Scan() {
		var ev testEvent
		if json.Unmarshal(lines.Bytes(), &ev) != nil {
			continue
		}
		pkg := cmp.Or(ev.Package, ev.ImportPath)
		switch {
		case ev.Action == "output" || ev.Action == "build-output":
			outputs[pkg] += ev.Output
		case ev.Action == "pass" && ev.Test == "":
			passed++
			fmt.Printf("pass  %s %.1fs\n", pkg, ev.Elapsed)
		case ev.Action == "fail" && ev.Test == "":
			fmt.Printf("FAIL  %s %.1fs\n", pkg, ev.Elapsed)
			order = append(order, pkg)
		case ev.Action == "fail":
			top, _, _ := strings.Cut(ev.Test, "/")
			if !slices.Contains(failed[pkg], top) {
				failed[pkg] = append(failed[pkg], top)
			}
		}
	}
	waitErr := cmd.Wait()
	if waitErr != nil && !errors.As(waitErr, new(*exec.ExitError)) {
		return nil, 0, waitErr
	}
	if len(order) == 0 && waitErr != nil {
		return []failure{{step: "test", pkg: "unknown", detail: lastLine(stderr.String())}}, passed, nil
	}
	var failures []failure
	for _, pkg := range order {
		f := failure{step: "test", pkg: pkg, tests: failed[pkg]}
		if len(f.tests) == 0 {
			f.detail = lastLine(outputs[pkg] + stderr.String())
		} else {
			f.flaky, f.tests = r.rerun(pkg, f.tests)
		}
		failures = append(failures, f)
	}
	return failures, passed, nil
}

func (r runner) rerun(pkg string, tests []string) (bool, []string) {
	var again []string
	for _, name := range tests {
		result := "pass"
		if r.goCommand("test", "-count=1", "-run", "^"+name+"$", pkg).Run() != nil {
			result, again = "FAIL", append(again, name)
		}
		fmt.Printf("rerun %s %s alone: %s\n", pkg, name, result)
	}
	if len(again) == 0 {
		return true, tests
	}
	return false, again
}

func stamp(repo, tree, head string, passed int, failures []failure) (bool, error) {
	after, dirty, err := guard.HeadTree(repo)
	if err != nil || after != tree || len(dirty) > 0 {
		fmt.Printf("no stamp: the run was not on a clean HEAD from start to end: tree %s then %s, %d uncommitted Go files\n", tree, after, len(dirty))
		return false, err
	}
	lines := []string{"tree " + tree, "head " + head, "date " + time.Now().Format(time.RFC3339), fmt.Sprintf("vet and test pass, %d packages", passed)}
	for _, f := range failures {
		lines = append(lines, strings.Join(append([]string{f.result(), f.step, f.pkg}, f.tests...), " "))
	}
	path := guard.StampPath(repo, guard.TreewideStamps, tree)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	fmt.Println("stamp", path)
	return true, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

func lastLine(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
