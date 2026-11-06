package main

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type row struct {
	task, arm, build, driver, id string
	tools                        []string
	sizes                        []int
	answerAt                     int
	failed                       string
}

func (r row) bytes() int {
	total := 0
	for _, size := range r.sizes {
		total += size
	}
	return total
}

type machine struct {
	exe      map[string]string
	fixtures string
	scratch  string
	env      []string
}

const runTimeout = 5 * time.Minute

func main() {
	old := flag.String("old", "", "a tofu built from "+oldCommit+", the 0.5.21 release")
	dev := flag.String("dev", "", "a tofu built from the current tree")
	fixtures := flag.String("fixtures", "bench/suites/tools/fixtures", "the fixture projects, copied fresh for every arm")
	scratch := flag.String("scratch", "", "a directory each arm gets a fresh project copy and a fresh HOME under")
	out := flag.String("out", "", "the report file, otherwise standard output")
	flag.Parse()
	if *old == "" || *dev == "" || *scratch == "" {
		fmt.Fprintln(os.Stderr, "usage: current -old <tofu 0.5.21 exe> -dev <tofu dev exe> -scratch <dir> [-fixtures <dir>] [-out <report.md>]")
		os.Exit(2)
	}
	began := time.Now()
	m, err := newMachine(*old, *dev, *fixtures, filepath.Join(*scratch, began.Format("20060102-150405")))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var rows []row
	for ti, t := range tasks() {
		for ai, a := range t.arms {
			rows = append(rows, m.cassette(t, a, fmt.Sprintf("%02d-%02d", ti, ai)))
			fmt.Fprintf(os.Stderr, "%s / %s: done\n", t.name, a.name)
		}
		if strings.HasPrefix(t.name, fixPrefix) {
			for bi, build := range []string{oldBuild, devBuild} {
				rows = append(rows, m.handCall(t, build, fmt.Sprintf("%02d-h%d", ti, bi)))
			}
		}
	}
	report := render(rows, began, m.commit(devBuild), m.scratch)
	if *out == "" {
		fmt.Print(report)
		return
	}
	if err := os.WriteFile(*out, []byte(report), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newMachine(old, dev, fixtures, scratch string) (machine, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return machine{}, err
	}
	modules, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		return machine{}, fmt.Errorf("go env GOMODCACHE: %w", err)
	}
	env := append(os.Environ(),
		"GOMODCACHE="+strings.TrimSpace(string(modules)),
		"GOPROXY=off",
		"GOMAXPROCS=4",
		"RUSTUP_HOME="+cmp.Or(os.Getenv("RUSTUP_HOME"), filepath.Join(home, ".rustup")),
		"CARGO_HOME="+cmp.Or(os.Getenv("CARGO_HOME"), filepath.Join(home, ".cargo")),
	)
	return machine{exe: map[string]string{oldBuild: old, devBuild: dev}, fixtures: fixtures, scratch: scratch, env: env}, nil
}

type place struct {
	dir, project, home string
	env                []string
}

func (m machine) fresh(t task, id string) (place, error) {
	dir := filepath.Join(m.scratch, id)
	p := place{dir: dir, project: filepath.Join(dir, t.fixture), home: filepath.Join(dir, "home")}
	p.env = slices.Concat(m.env, []string{"HOME=" + p.home, "USERPROFILE=" + p.home})
	if err := os.CopyFS(p.project, os.DirFS(filepath.Join(m.fixtures, t.fixture))); err != nil {
		return p, err
	}
	return p, os.MkdirAll(p.home, 0o755)
}

func (m machine) cassette(t task, a arm, id string) row {
	r := row{task: t.name, arm: a.name, build: a.build, driver: cassetteDriver, id: id}
	p, err := m.fresh(t, id)
	if err != nil {
		r.failed = err.Error()
		return r
	}
	var deck strings.Builder
	for _, s := range a.steps {
		fmt.Fprintf(&deck, `{"tools":[{"name":%q,"args":%s}]}`+"\n", s.tool, s.args)
	}
	deck.WriteString(`{"text":"done"}` + "\n")
	deckPath := filepath.Join(p.dir, "cassette.jsonl")
	if err := os.WriteFile(deckPath, []byte(deck.String()), 0o644); err != nil {
		r.failed = err.Error()
		return r
	}
	_, stderr, err := m.run(a.build, p.project, append(p.env, "TOFU_DRIVE_CASSETTE="+deckPath), "run", "--dir", ".", "--wire", "anthropic", "--no-gate", t.ask)
	if err != nil {
		r.failed = firstLine(stderr, err)
		return r
	}
	results, err := recorded(p.home)
	if err != nil {
		r.failed = err.Error()
		return r
	}
	answering := 0
	for _, s := range a.steps {
		if !s.cleanup {
			answering++
		}
	}
	if len(results) < answering {
		r.failed = fmt.Sprintf("%d of %d calls recorded a result", len(results), answering)
		return r
	}
	for at, one := range results[:answering] {
		r.tools, r.sizes = append(r.tools, a.steps[at].tool), append(r.sizes, len(one.content))
		if r.answerAt == 0 && t.answers(at, results) {
			r.answerAt = at + 1
		}
	}
	return r
}

func (m machine) handCall(t task, build, id string) row {
	r := row{task: t.name, arm: build + ", test", build: build, driver: "tofu tool", id: id}
	p, err := m.fresh(t, id)
	if err != nil {
		r.failed = err.Error()
		return r
	}
	stdout, stderr, err := m.run(build, p.project, p.env, "tool", "test", `{"path":"."}`)
	var exit *exec.ExitError
	if err != nil && !(errors.As(err, &exit) && exit.ExitCode() == 1) {
		r.failed = firstLine(stderr, err)
		return r
	}
	r.tools, r.sizes = []string{"test"}, []int{len(stdout)}
	if strings.Contains(stdout, "1 failed") {
		r.answerAt = 1
	}
	return r
}

func (m machine) run(build, dir string, env []string, args ...string) (string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, m.exe[build], args...)
	cmd.Dir, cmd.Env = dir, env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func (m machine) commit(build string) string {
	out, err := exec.Command(m.exe[build], "version").Output()
	if err != nil {
		return "unknown: " + err.Error()
	}
	for line := range strings.Lines(string(out)) {
		if value, found := strings.CutPrefix(line, "commit"); found {
			return strings.TrimSpace(value)
		}
	}
	return "unknown"
}

func recorded(home string) ([]result, error) {
	events, err := filepath.Glob(filepath.Join(home, ".tofu", "projects", "*", "sessions", "*", "events.jsonl"))
	if err != nil || len(events) != 1 {
		return nil, fmt.Errorf("want one recorded session under %s, found %d", home, len(events))
	}
	log, err := os.ReadFile(events[0])
	if err != nil {
		return nil, err
	}
	var results []result
	for line := range strings.Lines(string(log)) {
		var event struct {
			Kind string `json:"kind"`
			Body struct {
				Content string `json:"content"`
				Outcome string `json:"tool_outcome"`
			} `json:"body"`
		}
		if json.Unmarshal([]byte(line), &event) == nil && event.Kind == "tool_result" {
			results = append(results, result{event.Body.Content, event.Body.Outcome})
		}
	}
	return results, nil
}

func firstLine(stderr string, err error) string {
	line, _, _ := strings.Cut(strings.TrimSpace(stderr), "\n")
	return cmp.Or(line, err.Error())
}
