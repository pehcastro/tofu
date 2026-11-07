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
	"strings"
	"time"
)

type edit struct{ path, old, new string }

type scenario struct {
	language, fixture string
	source            string
	bug               map[string]string
	test, symbol      string
	breakAt, breakIf  string
	prints            []edit
	run               []string
	saw               map[string]*regexp.Regexp
}

type step struct {
	args    []string
	mayFail bool
	answer  bool
}

type outcome struct {
	language, arm string
	calls, bytes  int
	found         bool
	written, left int
	off           []string
	failed        string
	spent         time.Duration
}

type machine struct {
	exe, fixtures, scratch string
	env                    []string
}

const (
	armTrace      = "tofu tool debug: trace the function"
	armBreak      = "tofu tool debug: break on the third call"
	armPrintTool  = "print lines: tofu tool edit in, tofu tool bash run, tofu tool edit out"
	armPrintShell = "print lines: tofu tool edit in, the test run raw, tofu tool edit out"
	runTimeout    = 3 * time.Minute
)

var arms = []string{armTrace, armBreak, armPrintTool, armPrintShell}

var printedThird = regexp.MustCompile(`(?m)^ring 3 675\s*$`)

func scenarios() []scenario {
	return []scenario{
		{language: "go", fixture: "go", source: ".go", test: "TestTill", symbol: "orders.(*Till).Ring", breakAt: "till.go:14", breakIf: "t.rung == 3",
			bug: map[string]string{
				"till.go":      "package orders\n\ntype Till struct {\n\trung  int\n\ttotal int\n}\n\nfunc (t *Till) Ring(o Order) int {\n\tt.rung++\n\tcents := o.Total()\n\tif t.rung%3 == 0 {\n\t\tcents -= cents / 10\n\t}\n\tt.total += cents\n\treturn cents\n}\n",
				"till_test.go": "package orders\n\nimport \"testing\"\n\nfunc TestTill(t *testing.T) {\n\tvar till Till\n\tfor _, o := range []Order{{1, 250}, {2, 250}, {3, 250}, {4, 250}} {\n\t\ttill.Ring(o)\n\t}\n\tif till.total != 2500 {\n\t\tt.Fatalf(\"got %d, want %d\", till.total, 2500)\n\t}\n}\n",
			},
			prints: []edit{
				{"till.go", "\tt.total += cents\n", "\tfmt.Println(\"ring\", t.rung, cents)\n\tt.total += cents\n"},
				{"till.go", "package orders\n", "package orders\n\nimport \"fmt\"\n"},
			},
			run: []string{"go", "test", "-count=1", "-run", "^TestTill$", "."},
			saw: map[string]*regexp.Regexp{
				armTrace: regexp.MustCompile(`(?m)^  3\. .*Quantity: 3,.*returned 675 `),
				armBreak: regexp.MustCompile(`(?s)rung: 3,.*cents = 675 `),
			}},
		{language: "python", fixture: "python", source: ".py", test: "test_till.TillTest.test_till", symbol: "till.Till.ring", breakAt: "till.py:16", breakIf: "self.rung == 3",
			bug: map[string]string{
				"till.py":      "from dataclasses import dataclass\n\nfrom order import Order\n\n\n@dataclass\nclass Till:\n    rung: int = 0\n    total: int = 0\n\n    def ring(self, order: Order) -> int:\n        self.rung += 1\n        cents = order.total()\n        if self.rung % 3 == 0:\n            cents -= divmod(cents, 10)[0]\n        self.total += cents\n        return cents\n",
				"test_till.py": "import unittest\n\nfrom order import Order\nfrom till import Till\n\n\nclass TillTest(unittest.TestCase):\n    def test_till(self):\n        till = Till()\n        for quantity in (1, 2, 3, 4):\n            till.ring(Order(quantity=quantity, unit_cents=250))\n        self.assertEqual(till.total, 2500, f\"got {till.total}, want 2500\")\n",
			},
			prints: []edit{{"till.py", "        self.total += cents\n", "        print(\"ring\", self.rung, cents)\n        self.total += cents\n"}},
			run:    []string{"python", "-m", "unittest", "test_till.TillTest.test_till"},
			saw: map[string]*regexp.Regexp{
				armTrace: regexp.MustCompile(`(?m)^  3\. .*returned 675 `),
				armBreak: regexp.MustCompile(`(?s)rung.*3.*cents = 675 `),
			}},
	}
}

func main() {
	dev := flag.String("dev", "", "a tofu built from the current tree")
	fixtures := flag.String("fixtures", "bench/suites/tools/fixtures", "the fixture projects, copied fresh for every arm")
	scratch := flag.String("scratch", "", "a directory each arm gets a fresh project copy and a fresh HOME under")
	out := flag.String("out", "", "the report file, otherwise standard output")
	flag.Parse()
	if *dev == "" || *scratch == "" {
		fmt.Fprintln(os.Stderr, "usage: debug -dev <tofu dev exe> -scratch <dir> [-fixtures <dir>] [-out <report.md>]")
		os.Exit(2)
	}
	began := time.Now()
	m := machine{exe: *dev, fixtures: *fixtures, scratch: filepath.Join(*scratch, began.Format("20060102-150405")),
		env: append(os.Environ(), "GOPROXY=off", "GOMAXPROCS=4", "PYTHONDONTWRITEBYTECODE=1")}
	var rows []outcome
	for _, s := range scenarios() {
		for _, arm := range arms {
			rows = append(rows, m.measure(s, arm, fmt.Sprintf("%s-%d", s.language, len(rows))))
			fmt.Fprintf(os.Stderr, "%s, %s: done\n", s.language, arm)
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
	o := outcome{language: s.language, arm: arm}
	dir := filepath.Join(m.scratch, id)
	home, project := filepath.Join(dir, "home"), filepath.Join(dir, s.fixture)
	env := append(slices.Clone(m.env), "HOME="+home, "USERPROFILE="+home)
	err := errors.Join(os.CopyFS(project, os.DirFS(filepath.Join(m.fixtures, s.fixture))), os.MkdirAll(home, 0o755))
	for path, content := range s.bug {
		err = errors.Join(err, os.WriteFile(filepath.Join(project, path), []byte(content), 0o644))
	}
	before, snapErr := snapshot(project)
	if err = errors.Join(err, snapErr); err != nil {
		o.failed = err.Error()
		return o
	}
	saw, at := s.saw[arm], before
	if saw == nil {
		saw = printedThird
	}
	var heard strings.Builder
	started := time.Now()
	for _, st := range m.steps(s, arm) {
		stdout, output, err := run(project, env, st.args[0], st.args[1:]...)
		o.calls, o.bytes = o.calls+1, o.bytes+len(output)
		heard.WriteString(stdout)
		if err != nil && !st.mayFail {
			o.failed = firstLine(output, err)
			break
		}
		if st.answer {
			if at, err = snapshot(project); err != nil {
				o.failed = err.Error()
			}
		}
	}
	o.spent = time.Since(started)
	after, err := snapshot(project)
	if err != nil {
		o.failed = err.Error()
		return o
	}
	o.found = saw.MatchString(heard.String())
	o.written, o.left, o.off = added(before, at, s.source), added(before, after, s.source), differing(before, after)
	return o
}

func (m machine) steps(s scenario, arm string) []step {
	tool := func(name string, fields map[string]any, mayFail, answer bool) step {
		return step{args: []string{m.exe, "tool", name, arguments(fields)}, mayFail: mayFail, answer: answer}
	}
	debug := func(steps ...string) []step {
		return []step{tool("debug", map[string]any{"steps": append(append([]string{"start test " + s.test}, steps...), "stop")}, false, true)}
	}
	switch arm {
	case armTrace:
		return debug("trace " + s.symbol)
	case armBreak:
		return debug("break "+s.breakAt+" --if "+s.breakIf, "run")
	case armPrintTool, armPrintShell:
		var in, out []step
		for _, e := range s.prints {
			in = append(in, tool("edit", map[string]any{"path": e.path, "old_string": e.old, "new_string": e.new}, false, false))
			out = append(out, tool("edit", map[string]any{"path": e.path, "old_string": e.new, "new_string": e.old}, false, false))
		}
		runner := step{args: s.run, mayFail: true, answer: true}
		if arm == armPrintTool {
			runner = tool("bash", map[string]any{"command": strings.Join(s.run, " ")}, true, true)
		}
		return slices.Concat(in, []step{runner}, out)
	}
	panic("debug arm " + arm)
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
		if err != nil || entry.IsDir() {
			return err
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

func added(before, after map[string]string, source string) int {
	count := 0
	for path, content := range after {
		if !strings.HasSuffix(path, source) {
			continue
		}
		held := map[string]int{}
		for line := range strings.Lines(before[path]) {
			held[line]++
		}
		for line := range strings.Lines(content) {
			if held[line] > 0 {
				held[line]--
				continue
			}
			count++
		}
	}
	return count
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
