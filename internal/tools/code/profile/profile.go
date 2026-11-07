package profile

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"tofu/internal/models/llm/core"
	"tofu/internal/tools/contract"
)

const Name = "profile"

type kind string

const (
	cpu kind = "cpu"
	mem kind = "mem"
)

type runtime int

const (
	goTest runtime = iota
	node
	python
)

type Limits struct {
	Run               time.Duration
	Rows, CPUs, Lines int
}

type Profile struct {
	root   contract.Root
	limits Limits
}

func New(dir string, limits Limits) (contract.Tool, error) {
	root, err := contract.NewRoot(dir)
	return Profile{root: root, limits: limits}, err
}

func (Profile) Name() string { return Name }

func (p Profile) Definition() core.Tool {
	return core.Tool{
		Name: Name,
		Description: "runs one test or command under a profiler and prints the " + strconv.Itoa(p.limits.Rows) + " functions where the cpu time or the memory goes, each with the path:line it is defined at, flat (spent in the function itself) and cum (with what it calls), then the hottest call path. " +
			"Go runs through go test with -cpuprofile or -memprofile, Node through --cpu-prof or --heap-prof, Python through py-spy, cpu only. " +
			"use it instead of timing code by hand or reading raw pprof output: give command as words, like [\"go\",\"test\",\"./handler\",\"-run\",\"TestHandle\"] with one package, or [\"node\",\"app.js\"], or [\"python\",\"app.py\"]. a profiler missing from PATH is named with how to install it",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "the test or command as words, starting with go test, node or python"},
				"kind":    map[string]any{"type": "string", "enum": []string{string(cpu), string(mem)}, "description": "cpu for where the time goes, mem for where the allocations go"},
			},
			"required": []string{"command", "kind"},
		},
	}
}

func (p Profile) Run(ctx context.Context, raw json.RawMessage) (contract.Result, error) {
	var args struct {
		Command []string `json:"command"`
		Kind    kind     `json:"kind"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return contract.Result{}, fmt.Errorf("profile: arguments are not the expected shape: %w", err)
	}
	if args.Kind != cpu && args.Kind != mem {
		return refused("kind must be cpu or mem, not " + strconv.Quote(string(args.Kind))), nil
	}
	if len(args.Command) == 0 {
		return refused("command is required: go test with one package and -run, node with a script, or python with a script"), nil
	}
	r, refusal := runtimeOf(args.Command, args.Kind)
	if refusal != "" {
		return refused(refusal), nil
	}
	profiler, install := r.profiler()
	program, err := exec.LookPath(profiler)
	if err != nil {
		return refused(profiler + " is not on PATH: " + install + ", then run profile again"), nil
	}
	dir, err := os.MkdirTemp("", "tofu-profile-")
	if err != nil {
		return contract.Result{}, fmt.Errorf("profile: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	ctx, cancel := context.WithTimeout(ctx, p.limits.Run)
	defer cancel()
	argv := r.argv(args.Command, args.Kind, dir)
	cmd := exec.CommandContext(ctx, program, argv[1:]...)
	cmd.Dir = string(p.root)
	cmd.Env = append(os.Environ(), "GOMAXPROCS="+strconv.Itoa(p.limits.CPUs))
	out, err := cmd.CombinedOutput()
	ran := strings.Join(args.Command, " ")
	if err != nil {
		lines := strings.Split(strings.TrimRight(string(out), "\r\n"), "\n")
		return refused(ran + " failed, " + err.Error() + ":\n" + strings.Join(lines[max(0, len(lines)-p.limits.Lines):], "\n")), nil
	}
	taken, u, err := r.read(dir, args.Kind)
	if err != nil {
		return refused("reading the profile of " + ran + ": " + err.Error()), nil
	}
	if len(taken) == 0 {
		return refused(ran + " finished before the profiler took a sample: profile a test or command that runs longer"), nil
	}
	return contract.Result{Content: p.render(ran, args.Kind, taken, u), Command: string(args.Kind) + " " + ran}, nil
}

func refused(said string) contract.Result {
	return contract.Failure("profile: " + said + "\n")
}

func runtimeOf(command []string, k kind) (runtime, string) {
	switch strings.TrimSuffix(strings.ToLower(filepath.Base(command[0])), ".exe") {
	case "go":
		if len(command) < 2 || command[1] != "test" {
			return 0, "a Go profile runs go test: give go test, one package and -run with the test"
		}
		return goTest, ""
	case "node":
		return node, ""
	case "python", "python3", "py":
		if k == mem {
			return 0, "py-spy samples cpu time only: profile Python memory with tracemalloc or memray"
		}
		return python, ""
	}
	return 0, "profile runs go test, node or python, not " + strconv.Quote(command[0])
}

func (r runtime) profiler() (program, install string) {
	switch r {
	case goTest:
		return "go", "install Go from https://go.dev/dl"
	case node:
		return "node", "install Node.js from https://nodejs.org"
	}
	return "py-spy", "install it with pip install py-spy"
}

func (r runtime) argv(command []string, k kind, dir string) []string {
	written := filepath.Join(dir, "profile")
	switch r {
	case goTest:
		flags := []string{"-count=1", "-o", filepath.Join(dir, "profiled.exe"), "-cpuprofile", written}
		if k == mem {
			flags = append(flags[:3], "-memprofile", written, "-memprofilerate=1")
		}
		return slices.Concat(command[:2], flags, command[2:])
	case node:
		flag := "--cpu-prof"
		if k == mem {
			flag = "--heap-prof"
		}
		return slices.Concat(command[:1], []string{flag, flag + "-dir=" + dir}, command[1:])
	}
	return slices.Concat([]string{"py-spy", "record", "--format", "raw", "--function", "--output", written, "--"}, command)
}

func (r runtime) read(dir string, k kind) ([]sample, unit, error) {
	written := filepath.Join(dir, "profile")
	if r == node {
		found, _ := filepath.Glob(filepath.Join(dir, "*."+map[kind]string{cpu: "cpuprofile", mem: "heapprofile"}[k]))
		if len(found) == 0 {
			return nil, 0, fmt.Errorf("node wrote no profile into %s", dir)
		}
		written = found[0]
	}
	body, err := os.ReadFile(written)
	if err != nil {
		return nil, 0, err
	}
	switch {
	case r == goTest:
		return readPprof(body, k)
	case r == node && k == cpu:
		return readCPUProfile(body)
	case r == node:
		return readHeapProfile(body)
	}
	return readCollapsed(body)
}

type unit int

const (
	inNanoseconds unit = iota
	inBytes
	inSamples
)

func (u unit) name() string {
	return [...]string{"ms", "KiB", "samples"}[u]
}

func (u unit) format(v int64) string {
	if u == inSamples {
		return strconv.FormatInt(v, 10)
	}
	return strconv.FormatFloat(float64(v)/[...]float64{1e6, 1024}[u], 'f', 1, 64)
}

type frame struct {
	fn, file string
	line     int
}

type sample struct {
	stack []frame
	value int64
}

type row struct {
	frame
	flat, cum int64
}

func (p Profile) render(ran string, k kind, taken []sample, u unit) string {
	byFrame := map[frame]*row{}
	var rows []*row
	paths := map[string]int64{}
	var total int64
	for _, s := range taken {
		total += s.value
		counted := map[frame]bool{}
		names := make([]string, len(s.stack))
		for depth, f := range s.stack {
			r := byFrame[f]
			if r == nil {
				r = &row{frame: f}
				byFrame[f] = r
				rows = append(rows, r)
			}
			if depth == 0 {
				r.flat += s.value
			}
			if !counted[f] {
				r.cum += s.value
				counted[f] = true
			}
			names[len(s.stack)-1-depth] = f.fn
		}
		paths[strings.Join(names, " > ")] += s.value
	}
	slices.SortFunc(rows, func(a, b *row) int {
		return cmp.Or(cmp.Compare(b.flat, a.flat), cmp.Compare(b.cum, a.cum), strings.Compare(a.fn, b.fn))
	})
	hot := ""
	for path, value := range paths {
		if value > paths[hot] || value == paths[hot] && path < hot {
			hot = path
		}
	}
	shown := rows[:min(len(rows), p.limits.Rows)]
	percent := func(v int64) string { return strconv.FormatFloat(float64(v)*100/float64(total), 'f', 1, 64) + "%" }
	var out strings.Builder
	fmt.Fprintf(&out, "profile %s of %s: %s %s from %d recorded stacks, top %d of %d functions by flat\n", k, ran, u.format(total), u.name(), len(taken), len(shown), len(rows))
	table := tabwriter.NewWriter(&out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintf(table, "flat %s\tflat%%\tcum %s\tcum%%\tfunc\tpath:line\n", u.name(), u.name())
	for _, r := range shown {
		_, _ = fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\t%s\n", u.format(r.flat), percent(r.flat), u.format(r.cum), percent(r.cum), r.fn, p.place(r.frame))
	}
	_ = table.Flush()
	out.WriteString("hot path: " + hot + "\n")
	return out.String()
}

func (p Profile) place(f frame) string {
	if f.file == "" {
		return "-"
	}
	file := filepath.ToSlash(f.file)
	if rel, inside := contract.Within(string(p.root), filepath.FromSlash(f.file)); inside {
		file = filepath.ToSlash(rel)
	} else if parts := strings.Split(file, "/"); len(parts) > 4 {
		file = ".../" + strings.Join(parts[len(parts)-3:], "/")
	}
	return file + ":" + strconv.Itoa(f.line)
}
