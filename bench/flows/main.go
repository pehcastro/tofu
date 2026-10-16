package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
)

type row struct {
	number   int
	name     string
	passed   bool
	took     time.Duration
	evidence string
}

func main() {
	only := flag.String("only", "", "flow numbers to run, comma separated; a stamp needs every flow")
	src := flag.String("src", "", "the source tree tofu is built from; the repository when empty, and only the repository earns a stamp")
	keep := flag.Bool("keep", false, "keep the scratch homes, projects and builds")
	flag.Parse()
	failed, err := run(*only, *src, *keep)
	if err != nil {
		fmt.Fprintln(os.Stderr, "flows:", err)
		os.Exit(2)
	}
	if failed {
		os.Exit(1)
	}
}

func run(only, src string, keep bool) (bool, error) {
	repo, err := git("", nil, "rev-parse", "--show-toplevel")
	if err != nil {
		return false, err
	}
	stamped := src == ""
	if stamped {
		src = repo
	}
	chosen, err := choose(only)
	if err != nil {
		return false, err
	}
	work, err := os.MkdirTemp("", "tofu-flows")
	if err != nil {
		return false, err
	}
	if keep {
		fmt.Println("scratch kept in", work)
	} else {
		defer func() { _ = os.RemoveAll(work) }()
	}
	tree, head := "none, built from "+src, "none"
	if stamped {
		if tree, err = workingTree(repo, work); err != nil {
			return false, err
		}
		head, _ = git(repo, nil, "rev-parse", "--short", "HEAD")
	}
	machine, _ := os.Hostname()
	fmt.Printf("flows: tree %s, HEAD %s, %s %s/%s %d cpu, GOMAXPROCS %s, %s\n",
		tree, head, machine, runtime.GOOS, runtime.GOARCH, runtime.NumCPU(), childProcs, time.Now().Format(time.RFC3339))
	fmt.Println("cassettes only, no network; every flow on a fresh HOME and project; no call is discarded as a warm up")
	builds := map[bool]string{}
	var rows []row
	for _, each := range chosen {
		exe, built := builds[each.seamed]
		if !built {
			started := time.Now()
			if exe, err = build(src, work, each.seamed); err != nil {
				return false, err
			}
			builds[each.seamed] = exe
			fmt.Printf("built %s in %s\n", exe, time.Since(started).Round(time.Second))
		}
		rows = append(rows, play(each, exe, filepath.Join(work, "flow"+strconv.Itoa(each.number))))
		printRow(rows[len(rows)-1])
	}
	failed := slices.ContainsFunc(rows, func(r row) bool { return !r.passed })
	switch {
	case failed:
		fmt.Println("no stamp: a flow failed")
	case !stamped || len(chosen) != len(flows):
		fmt.Println("no stamp: a stamp needs every flow on the repository's own tree")
	default:
		return false, stamp(repo, work, tree, head, rows)
	}
	return failed, nil
}

func choose(only string) ([]flow, error) {
	if only == "" {
		return flows, nil
	}
	var chosen []flow
	for _, field := range strings.Split(only, ",") {
		number, err := strconv.Atoi(strings.TrimSpace(field))
		if err != nil || number < 1 || number > len(flows) {
			return nil, fmt.Errorf("-only %q names no flow from 1 to %d", field, len(flows))
		}
		chosen = append(chosen, flows[number-1])
	}
	return chosen, nil
}

func play(each flow, exe, dir string) row {
	started := time.Now()
	s, err := fresh(exe, dir)
	evidence := ""
	if err == nil {
		evidence, err = each.run(s)
	}
	r := row{number: each.number, name: each.name, passed: err == nil, took: time.Since(started), evidence: evidence}
	if err != nil {
		r.evidence = err.Error()
	}
	return r
}

func printRow(r row) {
	result := "FAIL"
	if r.passed {
		result = "pass"
	}
	fmt.Printf("| %d | %s | %s | %4.0fs | %s |\n", r.number, r.name, result, r.took.Seconds(), r.evidence)
}

func git(dir string, env []string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir, cmd.Env = dir, append(os.Environ(), env...)
	out, err := cmd.Output()
	if err != nil {
		var exited *exec.ExitError
		if errors.As(err, &exited) {
			return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), exited.Stderr)
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func workingTree(repo, work string) (string, error) {
	index := []string{"GIT_INDEX_FILE=" + filepath.Join(work, "flows.index")}
	if _, err := git(repo, index, "read-tree", "HEAD"); err != nil {
		return "", err
	}
	if _, err := git(repo, index, "add", "-A", "--", "*.go"); err != nil {
		return "", err
	}
	return git(repo, index, "write-tree")
}

func stamp(repo, work, tree, head string, rows []row) error {
	after, err := workingTree(repo, work)
	if err != nil {
		return err
	}
	if after != tree {
		fmt.Printf("no stamp: the Go files changed during the run, from tree %s to %s\n", tree, after)
		return nil
	}
	lines := []string{"tree " + tree, "head " + head, "date " + time.Now().Format(time.RFC3339)}
	for _, r := range rows {
		lines = append(lines, fmt.Sprintf("flow %d pass %.0fs %s", r.number, r.took.Seconds(), r.name))
	}
	path := filepath.Join(repo, ".local", "flows", tree)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	fmt.Println("stamp", path)
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}
