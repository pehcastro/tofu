package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

type change struct{ path, old, new string }

type turn struct {
	message string
	edit    change
}

type scenario struct {
	language, name, fixture string
	turns                   []turn
	culprit                 int
	person                  change
	check                   []string
	bisect                  map[string]any
}

type outcome struct {
	language, scenario, arm    string
	culprit, culpritTurns      int
	culpritFile                string
	namedTurn                  int
	namedFile                  string
	nameCalls, nameBytes       int
	treeMoved                  []string
	restoreCalls, restoreBytes int
	off                        []string
	personKept                 bool
	refused, failed            string
	restored                   restoreState
	nameSpent, restoreSpent    time.Duration
}

type restoreState int

const (
	notTried restoreState = iota
	wrong
	refusedClean
	exact
)

func (r restoreState) String() string {
	switch r {
	case notTried:
		return "not tried"
	case wrong:
		return "wrong"
	case refusedClean:
		return "refused, nothing written"
	case exact:
		return "exact"
	}
	panic("restore state " + strconv.Itoa(int(r)))
}

type machine struct {
	exe, fixtures, scratch string
	env                    []string
}

type project struct {
	dir    string
	env    []string
	m      machine
	o      *outcome
	before map[string]string
}

const (
	armTool    = "tofu tool bisect, then trail restore"
	armDiff    = "git by hand, edits uncommitted: diff, stash a file, test, pop, then checkout"
	armCommits = "git by hand, a commit per turn: log, bisect run, reset, then revert"
	runTimeout = 3 * time.Minute
)

var (
	bisectCulprit = regexp.MustCompile(`(?m)^culprit: turn (\S+), (\d+) of (\d+)`)
	bisectFile    = regexp.MustCompile(`(?m)^  (\S+) changed by calls`)
	gitFirstBad   = regexp.MustCompile(`(?m)^([0-9a-f]{40}) is the first bad commit`)
	gitStatFile   = regexp.MustCompile(`(?m)^ (\S+) +\| `)
)

func scenarios() []scenario {
	goCheck, pyCheck := []string{"go", "test", "-count=1", "-run", "^TestTotal$", "."}, []string{"python", "-m", "unittest", "test_order.OrderTest.test_total"}
	goBisect := map[string]any{"check": "test", "name": "TestTotal"}
	pyBisect := map[string]any{"check": "command", "command": pyCheck}
	goTurns := func(third, fifth turn) []turn {
		return []turn{
			{"make the discount seventy five cents", change{"order.go", "return o.Total() - 100", "return o.Total() - 75"}},
			{"add a notes file", change{"NOTES.md", "", "notes\n"}},
			third,
			{"add a note field to orders", change{"order.go", "\tUnitCents int\n}", "\tUnitCents int\n\tNote      string\n}"}},
			fifth,
			{"add a label method", change{"order.go", "func (o Order) Discounted() int {", "func (o Order) Label() string {\n\treturn o.Note\n}\n\nfunc (o Order) Discounted() int {"}},
			{"add a readme", change{"README.md", "", "orders\n"}},
			{"point the shipping skip at the notes", change{"order_test.go", `t.Skip("shipping rates are not modelled yet")`, `t.Skip("shipping rates are not modelled yet, see NOTES.md")`}},
		}
	}
	pyTurns := func(third, fifth turn) []turn {
		return []turn{
			{"make the discount seventy five cents", change{"order.py", "return self.total() - 100", "return self.total() - 75"}},
			{"add a notes file", change{"NOTES.md", "", "notes\n"}},
			third,
			{"add a note field to orders", change{"order.py", "    unit_cents: int\n", "    unit_cents: int\n    note: str = \"\"\n"}},
			fifth,
			{"add a label method", change{"order.py", "    def discounted(self) -> int:", "    def label(self) -> str:\n        return self.note\n\n    def discounted(self) -> int:"}},
			{"add a readme", change{"README.md", "", "orders\n"}},
			{"point the shipping skip at the notes", change{"test_order.py", `@unittest.skip("shipping rates are not modelled yet")`, `@unittest.skip("shipping rates are not modelled yet, see NOTES.md")`}},
		}
	}
	person := change{".env.example", "ORDER_DB_PATH", "ORDER_REGION=eu\nORDER_DB_PATH"}
	alone, shared := "the breaking turn is the only one to touch its file", "turns before and after the breaking one edit its file"
	return []scenario{
		{language: "go", name: alone, fixture: "go", culprit: 3, person: person, check: goCheck, bisect: goBisect, turns: goTurns(
			turn{"round the pricing up a cent", change{"pricing/pricing.go", "return quantity * unitCents", "return quantity*unitCents + 1"}},
			turn{"add a changes file", change{"CHANGES.md", "", "changes\n"}})},
		{language: "go", name: shared, fixture: "go", culprit: 5, person: person, check: goCheck, bisect: goBisect, turns: goTurns(
			turn{"reorder the pricing product", change{"pricing/pricing.go", "return quantity * unitCents", "return unitCents * quantity"}},
			turn{"add the handling fee to the total", change{"order.go", "return pr.Total(o.Quantity, o.UnitCents)", "return pr.Total(o.Quantity, o.UnitCents) + 1"}})},
		{language: "python", name: alone, fixture: "python", culprit: 3, person: person, check: pyCheck, bisect: pyBisect, turns: pyTurns(
			turn{"round the pricing up a cent", change{"pricing/__init__.py", "return quantity * unit_cents", "return quantity * unit_cents + 1"}},
			turn{"add a changes file", change{"CHANGES.md", "", "changes\n"}})},
		{language: "python", name: shared, fixture: "python", culprit: 5, person: person, check: pyCheck, bisect: pyBisect, turns: pyTurns(
			turn{"reorder the pricing product", change{"pricing/__init__.py", "return quantity * unit_cents", "return unit_cents * quantity"}},
			turn{"add the handling fee to the total", change{"order.py", "return pr.total(self.quantity, self.unit_cents)", "return pr.total(self.quantity, self.unit_cents) + 1"}})},
	}
}

func main() {
	dev := flag.String("dev", "", "a tofu built from the current tree")
	fixtures := flag.String("fixtures", "bench/suites/tools/fixtures", "the fixture projects, copied fresh for every arm")
	scratch := flag.String("scratch", "", "a directory each arm gets a fresh project copy and a fresh HOME under")
	out := flag.String("out", "", "the report file, otherwise standard output")
	flag.Parse()
	if *dev == "" || *scratch == "" {
		fmt.Fprintln(os.Stderr, "usage: history -dev <tofu dev exe> -scratch <dir> [-fixtures <dir>] [-out <report.md>]")
		os.Exit(2)
	}
	began := time.Now()
	m := machine{exe: *dev, fixtures: *fixtures, scratch: filepath.Join(*scratch, began.Format("20060102-150405")),
		env: append(os.Environ(), "GOPROXY=off", "GOMAXPROCS=4", "PYTHONDONTWRITEBYTECODE=1",
			"GIT_AUTHOR_NAME=bench", "GIT_AUTHOR_EMAIL=bench@localhost", "GIT_COMMITTER_NAME=bench", "GIT_COMMITTER_EMAIL=bench@localhost")}
	var rows []outcome
	for i, s := range scenarios() {
		for _, arm := range []string{armTool, armDiff, armCommits} {
			rows = append(rows, m.measure(s, arm, fmt.Sprintf("%s-%d-%d", s.language, i, len(rows))))
			fmt.Fprintf(os.Stderr, "%s, %s, %s: done\n", s.language, s.name, arm)
		}
	}
	report := render(rows, began, m)
	if *out == "" {
		fmt.Print(report)
		return
	}
	if err := os.WriteFile(*out, []byte(report), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func (m machine) measure(s scenario, arm, id string) outcome {
	o := outcome{language: s.language, scenario: s.name, arm: arm, culprit: s.culprit, culpritTurns: len(s.turns), culpritFile: s.turns[s.culprit-1].edit.path}
	dir := filepath.Join(m.scratch, id)
	home := filepath.Join(dir, "home")
	p := project{dir: filepath.Join(dir, s.fixture), env: append(slices.Clone(m.env), "HOME="+home, "USERPROFILE="+home), m: m, o: &o}
	if err := errors.Join(os.CopyFS(p.dir, os.DirFS(filepath.Join(m.fixtures, s.fixture))), os.MkdirAll(home, 0o755), os.MkdirAll(filepath.Join(dir, "envhome"), 0o755)); err != nil {
		o.failed = err.Error()
		return o
	}
	expected, err := snapshot(p.dir)
	if err != nil {
		o.failed = err.Error()
		return o
	}
	if err := p.drive(s, home, arm == armCommits); err != nil {
		o.failed = err.Error()
		return o
	}
	for i, t := range s.turns {
		if i+1 != s.culprit {
			expected[t.edit.path] = applied(expected[t.edit.path], t.edit)
		}
	}
	expected[s.person.path] = applied(expected[s.person.path], s.person)
	if p.before, err = snapshot(p.dir); err != nil {
		o.failed = err.Error()
		return o
	}
	started := time.Now()
	switch arm {
	case armTool:
		p.byTool(s)
	case armDiff:
		p.byDiff(s)
	case armCommits:
		p.byCommits(s)
	default:
		panic("history arm " + arm)
	}
	after, err := snapshot(p.dir)
	if err != nil {
		o.failed = err.Error()
		return o
	}
	o.off = differing(expected, after)
	switch {
	case o.restoreCalls == 0:
		o.restored = notTried
	case len(o.off) == 0:
		o.restored = exact
	case o.refused != "" && len(differing(p.before, after)) == 0:
		o.restored = refusedClean
	default:
		o.restored = wrong
	}
	o.restoreSpent = time.Since(started) - o.nameSpent
	o.personKept = after[s.person.path] == expected[s.person.path]
	return o
}

func (p project) drive(s scenario, home string, commit bool) error {
	if err := p.gits([]string{"init", "--quiet"}, []string{"config", "core.autocrlf", "false"}, []string{"add", "-A"}, []string{"commit", "--quiet", "-m", "before the session"}); err != nil {
		return err
	}
	envhome := filepath.Join(filepath.Dir(p.dir), "envhome")
	for i, t := range s.turns {
		said := fmt.Sprintf("done with turn %d", i+1)
		tool := map[string]any{"name": "edit", "args": map[string]any{"path": t.edit.path, "old_string": t.edit.old, "new_string": t.edit.new}}
		if t.edit.old == "" {
			tool = map[string]any{"name": "write", "args": map[string]any{"path": t.edit.path, "content": t.edit.new}}
		}
		cassette := filepath.Join(filepath.Dir(p.dir), fmt.Sprintf("turn-%d.jsonl", i+1))
		script := filepath.Join(filepath.Dir(p.dir), fmt.Sprintf("turn-%d.txt", i+1))
		answers := arguments(map[string]any{"text": "editing " + t.edit.path, "tools": []any{tool}}) + "\n" + arguments(map[string]any{"text": said}) + "\n"
		if err := errors.Join(os.WriteFile(cassette, []byte(answers), 0o644), os.WriteFile(script, []byte("type "+t.message+"\nkey enter\nwait "+said+"\nidle\n"), 0o644)); err != nil {
			return err
		}
		session := "--continue"
		if i == 0 {
			session = "--fresh"
		}
		if _, output, err := run(p.dir, append(slices.Clone(p.env), "HOME="+envhome, "USERPROFILE="+envhome), p.m.exe, "drive", script, "--dir", p.dir, "--home", home, "--cassette", cassette, "--plain", session); err != nil {
			return fmt.Errorf("drive turn %d: %s", i+1, firstLine(output, err))
		}
		raw, err := os.ReadFile(filepath.Join(p.dir, filepath.FromSlash(t.edit.path)))
		if err != nil || !strings.Contains(string(raw), t.edit.new) {
			return fmt.Errorf("drive turn %d left %s without its edit", i+1, t.edit.path)
		}
		if commit {
			if err := p.gits([]string{"add", "-A"}, []string{"commit", "--quiet", "-m", t.message}); err != nil {
				return err
			}
		}
	}
	path := filepath.Join(p.dir, filepath.FromSlash(s.person.path))
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(applied(string(raw), s.person)), 0o644)
}

func (p project) gits(commands ...[]string) error {
	for _, args := range commands {
		if _, output, err := run(p.dir, p.env, "git", args...); err != nil {
			return fmt.Errorf("git %s: %s", args[0], firstLine(output, err))
		}
	}
	return nil
}

func (p project) call(restoring bool, name string, args ...string) (string, string, error) {
	stdout, output, err := run(p.dir, p.env, name, args...)
	if restoring {
		p.o.restoreCalls, p.o.restoreBytes = p.o.restoreCalls+1, p.o.restoreBytes+len(output)
	} else {
		p.o.nameCalls, p.o.nameBytes = p.o.nameCalls+1, p.o.nameBytes+len(output)
	}
	return stdout, output, err
}

func (p project) named(began time.Time) {
	p.o.nameSpent = time.Since(began)
	now, err := snapshot(p.dir)
	if err != nil {
		p.o.failed = err.Error()
	}
	p.o.treeMoved = differing(p.before, now)
}

func (p project) byTool(s scenario) {
	began := time.Now()
	stdout, output, err := p.call(false, p.m.exe, "tool", "bisect", arguments(s.bisect))
	found := bisectCulprit.FindStringSubmatch(stdout)
	p.named(began)
	if err != nil || found == nil {
		p.o.failed = firstLine(output, err)
		return
	}
	p.o.namedTurn, _ = strconv.Atoi(found[2])
	var ids []string
	for _, file := range bisectFile.FindAllStringSubmatch(stdout, -1) {
		p.o.namedFile = file[1]
		ids = append(ids, found[1]+":"+file[1])
	}
	if _, output, err := p.call(true, p.m.exe, "tool", "trail", arguments(map[string]any{"mode": "restore", "ids": ids})); err != nil {
		p.o.refused = firstLine(output, err)
	}
}

func (p project) byDiff(s scenario) {
	began := time.Now()
	listed, output, err := p.call(false, "git", "diff", "--name-only")
	if err != nil {
		p.o.failed = firstLine(output, err)
		p.named(began)
		return
	}
	for _, file := range strings.Fields(listed) {
		if _, output, err := p.call(false, "git", "stash", "push", "--quiet", "--", file); err != nil {
			p.o.failed = firstLine(output, err)
			break
		}
		_, _, failing := p.call(false, s.check[0], s.check[1:]...)
		if _, output, err := p.call(false, "git", "stash", "pop", "--quiet"); err != nil {
			p.o.failed = firstLine(output, err)
			break
		}
		if failing == nil {
			p.o.namedFile = file
			break
		}
	}
	p.named(began)
	if p.o.namedFile == "" {
		return
	}
	if _, output, err := p.call(true, "git", "checkout", "--", p.o.namedFile); err != nil {
		p.o.refused = firstLine(output, err)
	}
}

func (p project) byCommits(s scenario) {
	began := time.Now()
	logged, output, err := p.call(false, "git", "log", "--reverse", "--format=%H")
	commits := strings.Fields(logged)
	if err != nil || len(commits) < 2 {
		p.o.failed = firstLine(output, err)
		p.named(began)
		return
	}
	var bisected string
	for _, args := range [][]string{{"bisect", "start", "HEAD", commits[0]}, append([]string{"bisect", "run"}, s.check...), {"bisect", "reset"}} {
		stdout, output, err := p.call(false, "git", args...)
		if err != nil {
			p.o.failed = firstLine(output, err)
		}
		if args[1] == "run" {
			bisected = stdout
		}
	}
	found := gitFirstBad.FindStringSubmatch(bisected)
	if found == nil {
		p.o.failed = "git bisect run named no first bad commit"
		p.named(began)
		return
	}
	p.o.namedTurn = slices.Index(commits, found[1])
	for _, file := range gitStatFile.FindAllStringSubmatch(bisected, -1) {
		p.o.namedFile = file[1]
	}
	p.named(began)
	if _, output, err := p.call(true, "git", "revert", "--no-edit", found[1]); err != nil {
		p.o.refused = firstLine(output, err)
		_, _, _ = p.call(true, "git", "revert", "--abort")
	}
}

func applied(content string, c change) string {
	if c.old == "" {
		return c.new
	}
	if !strings.Contains(content, c.old) {
		panic("history scenario: " + c.path + " does not hold " + c.old)
	}
	return strings.Replace(content, c.old, c.new, 1)
}

func arguments(fields map[string]any) string {
	raw, err := json.Marshal(fields)
	if err != nil {
		panic(err)
	}
	return string(raw)
}

func snapshot(dir string) (map[string]string, error) {
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && slices.Contains([]string{".git", "__pycache__"}, entry.Name()) {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		files[filepath.ToSlash(rel)] = string(raw)
		return err
	})
	return files, err
}

func differing(want, got map[string]string) []string {
	var off []string
	union := maps.Clone(want)
	maps.Copy(union, got)
	for _, path := range slices.Sorted(maps.Keys(union)) {
		held, kept := want[path]
		now, present := got[path]
		if held != now || kept != present {
			off = append(off, path)
		}
	}
	return off
}

func run(dir string, env []string, name string, args ...string) (string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir, cmd.Env = dir, env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	out := strings.ReplaceAll(stdout.String(), "\r\n", "\n")
	return out, out + "\n" + strings.ReplaceAll(stderr.String(), "\r\n", "\n"), err
}

func firstLine(output string, err error) string {
	line, _, _ := strings.Cut(strings.TrimSpace(output), "\n")
	if strings.TrimSpace(line) == "" && err != nil {
		return err.Error()
	}
	return strings.TrimSpace(line)
}
