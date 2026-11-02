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

	"tofu/internal/guard"
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
	repo, err := git("", "rev-parse", "--show-toplevel")
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
	head := "none"
	if stamped {
		head, _ = git(repo, "rev-parse", "--short", "HEAD")
	}
	stamps, desk, err := stampings(repo, src, stamped)
	if err != nil {
		return false, err
	}
	machine, _ := os.Hostname()
	fmt.Printf("flows: tree %s, desk tree %s, HEAD %s, %s %s/%s %d cpu, GOMAXPROCS %s, %s\n",
		stamps[0].tree, stamps[1].tree, head, machine, runtime.GOOS, runtime.GOARCH, runtime.NumCPU(), childProcs, time.Now().Format(time.RFC3339))
	fmt.Println("cassettes only, no network; every flow on a fresh HOME and project; no call is discarded as a warm up")
	for _, each := range stamps {
		for _, line := range each.lines {
			fmt.Println(line)
		}
		if len(each.dirty) > 0 {
			fmt.Printf("no %s stamp will be written: %d uncommitted files under its tree, and a stamp is keyed by HEAD's tree; run in a clean clone of HEAD\n", each.kind, len(each.dirty))
		}
		if each.held != "" {
			fmt.Printf("no %s stamp will be written: %s\n", each.kind, each.held)
		}
	}
	builds := map[bool]string{}
	var rows []row
	for _, each := range chosen {
		exe, made := builds[each.seamed]
		if !made {
			started := time.Now()
			if exe, err = build(src, work, each.seamed); err != nil {
				return false, err
			}
			builds[each.seamed] = exe
			fmt.Printf("built %s in %s\n", exe, time.Since(started).Round(time.Second))
		}
		rows = append(rows, play(each, exe, desk, filepath.Join(work, "flow"+strconv.Itoa(each.number))))
		printRow(rows[len(rows)-1])
	}
	failed := slices.ContainsFunc(rows, func(r row) bool { return !r.passed })
	for _, each := range stamps {
		refused, err := each.settle(repo, head, stamped, rows)
		if err != nil {
			return false, err
		}
		failed = failed || refused
	}
	return failed, nil
}

func choose(only string) ([]flow, error) {
	every := slices.Concat(flows, deskFlows)
	if only == "" {
		return every, nil
	}
	var chosen []flow
	for _, field := range strings.Split(only, ",") {
		number, err := strconv.Atoi(strings.TrimSpace(field))
		if err != nil || number < 1 || number > len(every) {
			return nil, fmt.Errorf("-only %q names no flow from 1 to %d", field, len(every))
		}
		chosen = append(chosen, every[number-1])
	}
	return chosen, nil
}

func play(each flow, exe, desk, dir string) row {
	started := time.Now()
	s, err := fresh(exe, desk, dir)
	evidence := ""
	if err == nil {
		evidence, err = each.run(s)
		s.stopHost()
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

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
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

type stamping struct {
	kind         string
	flows        []flow
	read         func(repo string) (string, []string, error)
	tree, held   string
	dirty, lines []string
}

func stampings(repo, src string, stamped bool) ([]stamping, string, error) {
	code := stamping{kind: guard.FlowsStamps, flows: flows, read: guard.HeadTree, tree: "none, built from " + src}
	desk := stamping{kind: guard.DeskStamps, flows: deskFlows, read: guard.DeskTree, tree: "none"}
	exe, built, err := deskBuild(src)
	if err != nil {
		desk.held = err.Error()
	} else {
		desk.lines = []string{"desk " + exe + " built " + built.Format(time.RFC3339)}
	}
	if !stamped {
		return []stamping{code, desk}, exe, nil
	}
	if code.tree, code.dirty, err = code.read(repo); err != nil {
		return nil, "", err
	}
	if desk.tree, desk.dirty, err = desk.read(repo); err != nil {
		return nil, "", err
	}
	if exe != "" {
		desk.held, err = deskStale(repo, exe, built)
	}
	return []stamping{code, desk}, exe, err
}

func (k stamping) settle(repo, head string, stamped bool, rows []row) (bool, error) {
	ran := slices.DeleteFunc(slices.Clone(rows), func(r row) bool {
		return !slices.ContainsFunc(k.flows, func(f flow) bool { return f.number == r.number })
	})
	switch {
	case slices.ContainsFunc(ran, func(r row) bool { return !r.passed }):
		fmt.Printf("no %s stamp: a flow failed\n", k.kind)
		return false, nil
	case !stamped || len(ran) != len(k.flows):
		fmt.Printf("no %s stamp: a stamp needs every %s flow on the repository's own tree\n", k.kind, k.kind)
		return false, nil
	case k.held != "":
		fmt.Printf("no %s stamp: %s\n", k.kind, k.held)
		return true, nil
	}
	after, dirty, err := k.read(repo)
	if err != nil || after != k.tree || len(k.dirty)+len(dirty) > 0 {
		fmt.Printf("no %s stamp: the run was not on a clean HEAD from start to end: tree %s then %s, %d uncommitted files then %d\n", k.kind, k.tree, after, len(k.dirty), len(dirty))
		return true, err
	}
	lines := append([]string{"tree " + k.tree, "head " + head, "date " + time.Now().Format(time.RFC3339)}, k.lines...)
	for _, r := range ran {
		lines = append(lines, fmt.Sprintf("flow %d pass %.0fs %s", r.number, r.took.Seconds(), r.name))
	}
	path := guard.StampPath(repo, k.kind, k.tree)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	fmt.Println("stamp", path)
	return false, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}
