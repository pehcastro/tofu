package main

import (
	"bytes"
	"cmp"
	"context"
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

type language struct {
	name, fixture, declFile, symbol, to, qualifier, declared string
	exts                                                     []string
	sites, callers                                           []string
	check                                                    []string
}

type outcome struct {
	language, task, arm string
	calls, bytes        int
	want, missed, wrong []string
	build, failed       string
	spent               time.Duration
}

type machine struct {
	exe, fixtures, scratch string
	env                    []string
}

const (
	taskRename = "rename through an import alias"
	taskImpact = "find callers through an import alias"
	armNone    = "none, the untouched copy"
	armTool    = "tofu tool symbols"
	armBare    = "grep, the bare name"
	armScoped  = "grep, bare in its file, package-qualified elsewhere"
	armCalls   = "grep, the name then a paren"
	runTimeout = 3 * time.Minute
)

func languages() []language {
	return []language{
		{name: "go", fixture: "go", declFile: "pricing/pricing.go", symbol: "Total", to: "Sum", qualifier: "pricing.", declared: `\bfunc\s+Total\b`, exts: []string{".go"},
			sites: []string{"pricing/pricing.go:3", "order.go:11"}, callers: []string{"order.go:11"}, check: []string{"go", "vet", ".", "./pricing"}},
		{name: "typescript", fixture: "ts", declFile: "src/pricing.ts", symbol: "total", to: "sum", qualifier: "pricing.", declared: `\bfunction\s+total\b`, exts: []string{".ts"},
			sites: []string{"src/pricing.ts:1", "src/order.ts:13"}, callers: []string{"src/order.ts:13"},
			check: []string{"tsc", "--noEmit", "--strict", "--target", "es2022", "--module", "nodenext", "--allowImportingTsExtensions", "src/order.ts", "src/pricing.ts"}},
		{name: "rust", fixture: "rust", declFile: "src/pricing.rs", symbol: "total", to: "sum", qualifier: "pricing::", declared: `\bfn\s+total\b`, exts: []string{".rs"},
			sites: []string{"src/pricing.rs:1", "src/lib.rs:12"}, callers: []string{"src/lib.rs:12"}, check: []string{"cargo", "test", "--no-run", "--offline", "-q"}},
		{name: "python", fixture: "python", declFile: "pricing/__init__.py", symbol: "total", to: "sum", qualifier: "pricing.", declared: `\bdef\s+total\b`, exts: []string{".py"},
			sites: []string{"pricing/__init__.py:1", "order.py:12"}, callers: []string{"order.py:12"},
			check: []string{"python", "-c", "from order import Order; Order(3, 250).discounted()"}},
	}
}

func main() {
	dev := flag.String("dev", "", "a tofu built from the current tree")
	fixtures := flag.String("fixtures", "bench/suites/tools/fixtures", "the fixture projects, copied fresh for every arm")
	scratch := flag.String("scratch", "", "a directory each arm gets a fresh project copy and a fresh HOME under")
	servers := flag.String("servers", "", "directories put first on PATH for the language servers and tsc, in this system's list form")
	out := flag.String("out", "", "the report file, otherwise standard output")
	flag.Parse()
	if *dev == "" || *scratch == "" {
		fmt.Fprintln(os.Stderr, "usage: code -dev <tofu dev exe> -scratch <dir> [-servers <dirs>] [-fixtures <dir>] [-out <report.md>]")
		os.Exit(2)
	}
	began := time.Now()
	m, err := newMachine(*dev, *fixtures, filepath.Join(*scratch, began.Format("20060102-150405")), *servers)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var rows, untouched []outcome
	for _, lang := range languages() {
		untouched = append(untouched, m.rename(lang, armNone, lang.name+"-r0"))
		for i, arm := range []string{armTool, armBare, armScoped} {
			rows = append(rows, m.rename(lang, arm, fmt.Sprintf("%s-r%d", lang.name, i+1)))
		}
		for i, arm := range []string{armTool, armCalls} {
			rows = append(rows, m.impact(lang, arm, fmt.Sprintf("%s-i%d", lang.name, i+1)))
		}
		fmt.Fprintf(os.Stderr, "%s: done\n", lang.name)
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
	modules, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		return machine{}, fmt.Errorf("go env GOMODCACHE: %w", err)
	}
	if err := os.Setenv("PATH", strings.Join(append(filepath.SplitList(servers), os.Getenv("PATH")), string(os.PathListSeparator))); err != nil {
		return machine{}, err
	}
	env := append(os.Environ(),
		"GOMODCACHE="+strings.TrimSpace(string(modules)),
		"GOPROXY=off",
		"GOMAXPROCS=4",
		"RUSTUP_HOME="+cmp.Or(os.Getenv("RUSTUP_HOME"), filepath.Join(home, ".rustup")),
		"CARGO_HOME="+cmp.Or(os.Getenv("CARGO_HOME"), filepath.Join(home, ".cargo")),
	)
	return machine{exe: dev, fixtures: fixtures, scratch: scratch, env: env}, nil
}

func (m machine) fresh(lang language, id string) (string, []string, error) {
	dir := filepath.Join(m.scratch, id)
	project, home := filepath.Join(dir, lang.fixture), filepath.Join(dir, "home")
	env := append(slices.Clone(m.env), "HOME="+home, "USERPROFILE="+home)
	if err := os.CopyFS(project, os.DirFS(filepath.Join(m.fixtures, lang.fixture))); err != nil {
		return project, env, err
	}
	return project, env, os.MkdirAll(home, 0o755)
}

func (m machine) rename(lang language, arm, id string) outcome {
	o := outcome{language: lang.name, task: taskRename, arm: arm, want: lang.sites}
	project, env, err := m.fresh(lang, id)
	if err != nil {
		o.failed = err.Error()
		return o
	}
	before, err := snapshot(project, lang.exts)
	if err != nil {
		o.failed = err.Error()
		return o
	}
	started := time.Now()
	switch arm {
	case armNone:
	case armTool:
		o.calls = 1
		stdout, output, err := run(project, env, m.exe, "tool", "symbols", fmt.Sprintf(`{"name":%q,"path":%q,"mode":"rename","to":%q,"apply":true}`, lang.symbol, lang.declFile, lang.to))
		o.bytes = len(stdout)
		if err != nil {
			o.failed = firstLine(output, err)
		}
	case armBare, armScoped:
		o.calls = 2
		bare := regexp.MustCompile(`\b` + lang.symbol + `\b`)
		qualified := regexp.MustCompile(`\b` + regexp.QuoteMeta(lang.qualifier+lang.symbol) + `\b`)
		o.bytes, err = replace(project, before, func(rel string) (*regexp.Regexp, string) {
			if arm == armScoped && rel != lang.declFile {
				return qualified, lang.qualifier + lang.to
			}
			return bare, lang.to
		})
		if err != nil {
			o.failed = err.Error()
		}
	default:
		panic("rename arm " + arm)
	}
	o.spent = time.Since(started)
	after, err := snapshot(project, lang.exts)
	if err != nil {
		o.failed = cmp.Or(o.failed, err.Error())
		return o
	}
	o.missed, o.wrong = score(lang.sites, changed(before, after))
	_, output, err := run(project, env, lang.check[0], lang.check[1:]...)
	o.build = "passes"
	if err != nil {
		o.build = "fails: " + errorLine(output, err)
	}
	return o
}

func (m machine) impact(lang language, arm, id string) outcome {
	o := outcome{language: lang.name, task: taskImpact, arm: arm, want: lang.callers, calls: 1}
	project, env, err := m.fresh(lang, id)
	if err != nil {
		o.failed = err.Error()
		return o
	}
	var found []string
	started := time.Now()
	switch arm {
	case armTool:
		stdout, output, err := run(project, env, m.exe, "tool", "symbols", fmt.Sprintf(`{"name":%q,"path":%q,"mode":"impact"}`, lang.symbol, lang.declFile))
		o.bytes = len(stdout)
		if err != nil {
			o.failed = firstLine(output, err)
		}
		found = callersSection(stdout)
	case armCalls:
		files, err := snapshot(project, lang.exts)
		if err != nil {
			o.failed = err.Error()
			break
		}
		call, declared := regexp.MustCompile(`\b`+lang.symbol+`\s*\(`), regexp.MustCompile(lang.declared)
		for _, rel := range slices.Sorted(maps.Keys(files)) {
			for number, line := range files[rel] {
				if call.MatchString(line) && !declared.MatchString(line) {
					found = append(found, fmt.Sprintf("%s:%d", rel, number+1))
					o.bytes += len(fmt.Sprintf("%s:%d:%s\n", rel, number+1, line))
				}
			}
		}
	default:
		panic("impact arm " + arm)
	}
	o.spent = time.Since(started)
	o.missed, o.wrong = score(lang.callers, found)
	return o
}

func callersSection(stdout string) []string {
	site := regexp.MustCompile(`^\s+\S+\s+(\S+:\d+)`)
	var found []string
	inside := false
	for line := range strings.Lines(stdout) {
		line = strings.TrimRight(line, "\r\n")
		switch {
		case line == "callers":
			inside = true
		case strings.TrimSpace(line) == "":
			inside = false
		case inside:
			if match := site.FindStringSubmatch(line); match != nil {
				found = append(found, match[1])
			}
		}
	}
	return found
}

func snapshot(project string, exts []string) (map[string][]string, error) {
	files := map[string][]string{}
	err := filepath.WalkDir(project, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && slices.Contains([]string{"node_modules", "target", ".git"}, entry.Name()) {
			return filepath.SkipDir
		}
		if entry.IsDir() || !slices.Contains(exts, filepath.Ext(path)) {
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

func replace(project string, files map[string][]string, pattern func(rel string) (*regexp.Regexp, string)) (int, error) {
	listed := 0
	for _, rel := range slices.Sorted(maps.Keys(files)) {
		match, to := pattern(rel)
		lines := slices.Clone(files[rel])
		for number, line := range lines {
			if match.MatchString(line) {
				listed += len(fmt.Sprintf("%s:%d:%s\n", rel, number+1, line))
				lines[number] = match.ReplaceAllLiteralString(line, to)
			}
		}
		if slices.Equal(lines, files[rel]) {
			continue
		}
		if err := os.WriteFile(filepath.Join(project, rel), []byte(strings.Join(lines, "\n")), 0o644); err != nil {
			return listed, err
		}
	}
	return listed, nil
}

func changed(before, after map[string][]string) []string {
	var sites []string
	for _, rel := range slices.Sorted(maps.Keys(after)) {
		old, now := before[rel], after[rel]
		for number := range max(len(old), len(now)) {
			if number >= len(old) || number >= len(now) || old[number] != now[number] {
				sites = append(sites, fmt.Sprintf("%s:%d", rel, number+1))
			}
		}
	}
	return sites
}

func score(want, got []string) (missed, wrong []string) {
	for _, site := range want {
		if !slices.Contains(got, site) {
			missed = append(missed, site)
		}
	}
	for _, site := range got {
		if !slices.Contains(want, site) {
			wrong = append(wrong, site)
		}
	}
	return missed, wrong
}

func run(dir string, env []string, name string, args ...string) (string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir, cmd.Env = dir, env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	return stdout.String(), stdout.String() + "\n" + stderr.String(), err
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
