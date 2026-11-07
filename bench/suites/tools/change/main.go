package main

import (
	"bytes"
	"cmp"
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

type task struct {
	language, name, fixture, file string
	pattern, rewrite, lang        string
	old, new                      string
	want                          map[int]string
}

type tally struct {
	command                []string
	pass, fail, skip, nums *regexp.Regexp
}

type outcome struct {
	language, task, arm string
	calls, bytes        int
	want                int
	missed, wrong       []string
	tests, failed       string
	spent               time.Duration
}

type machine struct {
	exe, fixtures, scratch string
	env                    []string
}

const (
	taskRename = "rename the local got to actual, message text kept"
	taskCall   = "change the assertion call form"
	armNone    = "none, the untouched copy"
	armTool    = "tofu tool codemod, preview then apply"
	armEdit    = "tofu tool edit, replace_all on the literal"
	runTimeout = 3 * time.Minute
)

func tasks() []task {
	goTest, tsTest, pyTest, rsTest := "order_test.go", "src/order.test.ts", "test_order.py", "src/lib.rs"
	return []task{
		{language: "go", name: taskRename, fixture: "go", file: goTest, pattern: "got", rewrite: "actual", lang: "go", old: "got", new: "actual", want: map[int]string{
			6: "\tactual := Order{Quantity: 3, UnitCents: 250}.Total()", 8: "\tif actual != want {", 9: "\t\tt.Fatalf(\"got %d, want %d\", actual, want)",
			14: "\tactual := Order{Quantity: 3, UnitCents: 250}.Discounted()", 16: "\tif actual != want {", 17: "\t\tt.Fatalf(\"got %d, want %d\", actual, want)"}},
		{language: "go", name: taskCall, fixture: "go", file: goTest, pattern: "t.Fatalf($$$A)", rewrite: "t.Errorf($$$A)", lang: "go", old: "t.Fatalf(", new: "t.Errorf(", want: map[int]string{
			9: "\t\tt.Errorf(\"got %d, want %d\", got, want)", 17: "\t\tt.Errorf(\"got %d, want %d\", got, want)"}},
		{language: "typescript", name: taskRename, fixture: "ts", file: tsTest, pattern: "got", rewrite: "actual", lang: "ts", old: "got", new: "actual", want: map[int]string{
			6: "  const actual = new Order(3, 250).total();", 8: "  assert.equal(actual, want, `got ${actual}, want ${want}`);",
			12: "  const actual = new Order(3, 250).discounted();", 14: "  assert.equal(actual, want, `got ${actual}, want ${want}`);"}},
		{language: "typescript", name: taskCall, fixture: "ts", file: tsTest, pattern: "assert.equal($$$A)", rewrite: "assert.strictEqual($$$A)", lang: "ts", old: "assert.equal(", new: "assert.strictEqual(", want: map[int]string{
			8: "  assert.strictEqual(got, want, `got ${got}, want ${want}`);", 14: "  assert.strictEqual(got, want, `got ${got}, want ${want}`);"}},
		{language: "python", name: taskRename, fixture: "python", file: pyTest, pattern: "got", rewrite: "actual", lang: "python", old: "got", new: "actual", want: map[int]string{
			8: "        actual = Order(quantity=3, unit_cents=250).total()", 10: "        self.assertEqual(actual, want, f\"got {actual}, want {want}\")",
			13: "        actual = Order(quantity=3, unit_cents=250).discounted()", 15: "        self.assertEqual(actual, want, f\"got {actual}, want {want}\")"}},
		{language: "python", name: taskCall, fixture: "python", file: pyTest, pattern: "self.assertEqual($A, $B, $M)", rewrite: "self.assertEqual($A, $B, msg=$M)", lang: "python", old: "want, f\"", new: "want, msg=f\"", want: map[int]string{
			10: "        self.assertEqual(got, want, msg=f\"got {got}, want {want}\")", 15: "        self.assertEqual(got, want, msg=f\"got {got}, want {want}\")"}},
		{language: "rust", name: taskRename, fixture: "rust", file: rsTest, pattern: "got", rewrite: "actual", lang: "rust", old: "got", new: "actual", want: map[int]string{
			26: "        let actual = Order { quantity: 3, unit_cents: 250 }.total();", 28: "        assert_eq!(actual, want, \"got {actual}, want {want}\");",
			33: "        let actual = Order { quantity: 3, unit_cents: 250 }.discounted();", 35: "        assert_eq!(actual, want, \"got {actual}, want {want}\");"}},
		{language: "rust", name: taskCall, fixture: "rust", file: rsTest, pattern: "assert_eq!($A, $B, $M)", rewrite: "assert!($A == $B, $M)", lang: "rust", old: "assert_eq!(got, want, ", new: "assert!(got == want, ", want: map[int]string{
			28: "        assert!(got == want, \"got {got}, want {want}\");", 35: "        assert!(got == want, \"got {got}, want {want}\");"}},
	}
}

func tallies() map[string]tally {
	return map[string]tally{
		"go":         {command: []string{"go", "test", "-v", "."}, pass: regexp.MustCompile(`(?m)^--- PASS`), fail: regexp.MustCompile(`(?m)^--- FAIL`), skip: regexp.MustCompile(`(?m)^--- SKIP`)},
		"typescript": {command: []string{"node", "--test", "--test-reporter=tap", "src/order.test.ts"}, nums: regexp.MustCompile(`(?m)^# (pass|fail|skipped) (\d+)`)},
		"python":     {command: []string{"python", "-m", "unittest", "-v"}, pass: regexp.MustCompile(`(?m) \.\.\. ok$`), fail: regexp.MustCompile(`(?m) \.\.\. (FAIL|ERROR)$`), skip: regexp.MustCompile(`(?m) \.\.\. skipped`)},
		"rust":       {command: []string{"cargo", "test", "--offline"}, pass: regexp.MustCompile(`(?m)^test \S+ \.\.\. ok$`), fail: regexp.MustCompile(`(?m)^test \S+ \.\.\. FAILED$`), skip: regexp.MustCompile(`(?m)^test \S+ \.\.\. ignored`)},
	}
}

func main() {
	dev := flag.String("dev", "", "a tofu built from the current tree")
	fixtures := flag.String("fixtures", "bench/suites/tools/fixtures", "the fixture projects, copied fresh for every arm")
	scratch := flag.String("scratch", "", "a directory each arm gets a fresh project copy and a fresh HOME under")
	servers := flag.String("servers", "", "directories put first on PATH for tsc, in this system's list form")
	out := flag.String("out", "", "the report file, otherwise standard output")
	flag.Parse()
	if *dev == "" || *scratch == "" {
		fmt.Fprintln(os.Stderr, "usage: change -dev <tofu dev exe> -scratch <dir> [-servers <dirs>] [-fixtures <dir>] [-out <report.md>]")
		os.Exit(2)
	}
	began := time.Now()
	m, err := newMachine(*dev, *fixtures, filepath.Join(*scratch, began.Format("20060102-150405")), *servers)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var rows, untouched []outcome
	for i, t := range tasks() {
		if !slices.ContainsFunc(untouched, func(o outcome) bool { return o.language == t.language }) {
			untouched = append(untouched, m.change(t, armNone, fmt.Sprintf("%s-0", t.language)))
		}
		rows = append(rows, m.change(t, armTool, fmt.Sprintf("%s-%d-tool", t.language, i)), m.change(t, armEdit, fmt.Sprintf("%s-%d-edit", t.language, i)))
		fmt.Fprintf(os.Stderr, "%s, %s: done\n", t.language, t.name)
	}
	report := render(rows, untouched, began, m)
	if *out == "" {
		fmt.Print(report)
		return
	}
	if err := os.WriteFile(*out, []byte(report), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newMachine(dev, fixtures, scratch, servers string) (machine, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return machine{}, err
	}
	if err := os.Setenv("PATH", strings.Join(append(filepath.SplitList(servers), os.Getenv("PATH")), string(os.PathListSeparator))); err != nil {
		return machine{}, err
	}
	env := append(os.Environ(),
		"GOPROXY=off",
		"GOMAXPROCS=4",
		"RUSTUP_HOME="+cmp.Or(os.Getenv("RUSTUP_HOME"), filepath.Join(home, ".rustup")),
		"CARGO_HOME="+cmp.Or(os.Getenv("CARGO_HOME"), filepath.Join(home, ".cargo")),
	)
	return machine{exe: dev, fixtures: fixtures, scratch: scratch, env: env}, nil
}

func (m machine) change(t task, arm, id string) outcome {
	o := outcome{language: t.language, task: t.name, arm: arm, want: len(t.want)}
	dir := filepath.Join(m.scratch, id)
	project, home := filepath.Join(dir, t.fixture), filepath.Join(dir, "home")
	env := append(slices.Clone(m.env), "HOME="+home, "USERPROFILE="+home)
	if err := errors.Join(os.CopyFS(project, os.DirFS(filepath.Join(m.fixtures, t.fixture))), os.MkdirAll(home, 0o755)); err != nil {
		o.failed = err.Error()
		return o
	}
	before, err := snapshot(project)
	if err != nil {
		o.failed = err.Error()
		return o
	}
	started := time.Now()
	switch arm {
	case armNone:
	case armTool:
		o.calls = 1
		preview, output, err := run(project, env, m.exe, "tool", "codemod", arguments(map[string]any{"pattern": t.pattern, "rewrite": t.rewrite, "lang": t.lang}))
		o.bytes = len(preview)
		found := regexp.MustCompile(`^codemod: (\d+) match`).FindStringSubmatch(preview)
		if err != nil || found == nil {
			o.failed = firstLine(output, err)
			break
		}
		expect, _ := strconv.Atoi(found[1])
		o.calls = 2
		applied, output, err := run(project, env, m.exe, "tool", "codemod", arguments(map[string]any{"pattern": t.pattern, "rewrite": t.rewrite, "lang": t.lang, "apply": true, "expect": expect}))
		o.bytes += len(applied)
		if err != nil {
			o.failed = firstLine(output, err)
		}
	case armEdit:
		o.calls = 1
		stdout, output, err := run(project, env, m.exe, "tool", "edit", arguments(map[string]any{"path": t.file, "old_string": t.old, "new_string": t.new, "replace_all": true}))
		o.bytes = len(stdout)
		if err != nil {
			o.failed = firstLine(output, err)
		}
	default:
		panic("change arm " + arm)
	}
	o.spent = time.Since(started)
	after, err := snapshot(project)
	if err != nil {
		o.failed = cmp.Or(o.failed, err.Error())
		return o
	}
	o.missed, o.wrong = score(t, before, after)
	o.tests = tests(tallies()[t.language], project, env)
	return o
}

func arguments(fields map[string]any) string {
	raw, err := json.Marshal(fields)
	if err != nil {
		panic(err)
	}
	return string(raw)
}

func snapshot(project string) (map[string][]string, error) {
	files := map[string][]string{}
	err := filepath.WalkDir(project, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && slices.Contains([]string{"node_modules", "target", ".git"}, entry.Name()) {
			return filepath.SkipDir
		}
		if entry.IsDir() || !slices.Contains([]string{".go", ".ts", ".py", ".rs"}, filepath.Ext(path)) {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(project, path)
		files[filepath.ToSlash(rel)] = strings.Split(string(raw), "\n")
		return err
	})
	return files, err
}

func score(t task, before, after map[string][]string) (missed, wrong []string) {
	for _, line := range slices.Sorted(maps.Keys(t.want)) {
		got := after[t.file]
		if line > len(got) || got[line-1] == before[t.file][line-1] {
			missed = append(missed, fmt.Sprintf("%s:%d", t.file, line))
		}
	}
	for _, rel := range slices.Sorted(maps.Keys(after)) {
		old, now := before[rel], after[rel]
		for number := range max(len(old), len(now)) {
			if number < len(old) && number < len(now) && old[number] == now[number] {
				continue
			}
			if wanted, listed := t.want[number+1]; rel == t.file && listed && number < len(now) && wanted == now[number] {
				continue
			}
			wrong = append(wrong, fmt.Sprintf("%s:%d", rel, number+1))
		}
	}
	return missed, wrong
}

func tests(c tally, project string, env []string) string {
	stdout, output, err := run(project, env, c.command[0], c.command[1:]...)
	counts := map[string]int{}
	if c.nums != nil {
		for _, found := range c.nums.FindAllStringSubmatch(stdout, -1) {
			counts[found[1]], _ = strconv.Atoi(found[2])
		}
	} else {
		counts["pass"], counts["fail"], counts["skipped"] = len(c.pass.FindAllString(output, -1)), len(c.fail.FindAllString(output, -1)), len(c.skip.FindAllString(output, -1))
	}
	if counts["pass"]+counts["fail"]+counts["skipped"] == 0 {
		return "does not build: " + errorLine(output, err)
	}
	return fmt.Sprintf("pass %d fail %d skip %d", counts["pass"], counts["fail"], counts["skipped"])
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

func errorLine(output string, err error) string {
	for line := range strings.Lines(output) {
		if lower := strings.ToLower(line); strings.Contains(lower, "error") || strings.Contains(lower, "undefined") {
			return strings.TrimSpace(line)
		}
	}
	return firstLine(output, err)
}

func firstLine(output string, err error) string {
	line, _, _ := strings.Cut(strings.TrimSpace(output), "\n")
	return cmp.Or(strings.TrimSpace(line), err.Error())
}
