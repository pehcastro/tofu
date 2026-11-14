package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"tofu/internal/konst"
	platformfs "tofu/internal/platform/fs"
	"tofu/internal/project/guard"
	"tofu/internal/stat"
)

const (
	childProcs        = "4"
	baselineFile      = "bench/gates/walls/baseline.json"
	noiseFloorMS      = 25
	noiseFloorPercent = 25
)

type tierLimit struct {
	tier  string
	under int64
}

var tierLimits = []tierLimit{{"S", 100}, {"A", 300}, {"B", 700}, {"C", 1000}, {"D", 1500}, {"E", math.MaxInt64}}

type opName struct{ op, name string }

type measured struct {
	Op     string `json:"op"`
	Name   string `json:"name"`
	N      int    `json:"n"`
	OwnP50 int64  `json:"own_p50"`
	Tier   string `json:"tier"`
}

type baseline struct {
	Machine string     `json:"machine"`
	Date    string     `json:"date"`
	Head    string     `json:"head"`
	Runs    int        `json:"runs"`
	Ops     []measured `json:"ops"`
}

type gate struct {
	repo, head, tree, machine string
	dirty                     []string
	stamped                   bool
}

func main() {
	update := flag.Bool("update", false, "rewrite bench/gates/walls/baseline.json from this run instead of comparing")
	src := flag.String("src", "", "the source tree tofu is built from; the repository when empty, and only the repository earns a stamp")
	runs := flag.Int("runs", 5, "times every operation runs; the own p50 is over these")
	keep := flag.Bool("keep", false, "keep the scratch home, project and build")
	flag.Parse()
	started := time.Now()
	failed, err := run(*update, *src, *runs, *keep)
	fmt.Printf("walls: whole run %s\n", time.Since(started).Round(100*time.Millisecond))
	if err != nil {
		fmt.Fprintln(os.Stderr, "walls:", err)
		os.Exit(2)
	}
	if failed {
		os.Exit(1)
	}
}

func run(update bool, src string, runs int, keep bool) (bool, error) {
	repo, err := git("", "rev-parse", "--show-toplevel")
	if err != nil {
		return false, err
	}
	g := gate{repo: repo, tree: "none", stamped: src == "" && !update}
	g.head, _ = git(repo, "rev-parse", "--short", "HEAD")
	g.machine, _ = os.Hostname()
	if g.stamped {
		if g.tree, g.dirty, err = guard.HeadTree(repo); err != nil {
			return false, err
		}
	}
	src = cmp.Or(src, repo)
	work, err := os.MkdirTemp("", "tofu-walls")
	if err != nil {
		return false, err
	}
	if keep {
		fmt.Println("scratch kept in", work)
	} else {
		defer func() { _ = os.RemoveAll(work) }()
	}
	fmt.Printf("walls: tree %s, HEAD %s, built from %s, %s %s/%s %d cpu, GOMAXPROCS %s, %s\n",
		g.tree, g.head, src, g.machine, runtime.GOOS, runtime.GOARCH, runtime.NumCPU(), childProcs, time.Now().Format(time.RFC3339))
	fmt.Printf("cassettes only, no network; one fresh HOME and project; %d runs of every op; own = ms - outside_ms, 0 when absent; no call is discarded as a warm up\n", runs)
	fmt.Printf("an op fails when its own p50 moves to a worse tier than the baseline's and rose more than %d ms or %d%% of the baseline, whichever is more\n", noiseFloorMS, noiseFloorPercent)
	if len(g.dirty) > 0 {
		fmt.Printf("no walls stamp will be written: uncommitted %s, and a stamp is keyed by HEAD's tree; run in a clean clone of HEAD\n", strings.Join(g.dirty, ", "))
	}
	building := time.Now()
	exe, err := build(src, work)
	if err != nil {
		return false, err
	}
	fmt.Printf("built %s in %s\n", exe, time.Since(building).Round(time.Second))
	first, err := fresh(exe, work, "turn.jsonl", "frame.steps")
	if err != nil {
		return false, err
	}
	more, err := fresh(exe, filepath.Join(work, "more"), "more.jsonl", "frame.steps")
	if err == nil {
		err = more.hooked()
	}
	if err != nil {
		return false, err
	}
	sifted, err := fresh(exe, filepath.Join(work, "sift"), "sift.jsonl", "sift.steps")
	if err != nil {
		return false, err
	}
	fmt.Printf("the first home drives a lead turn, then a second turn.send in the same session; of the second turn only its %v rows are kept\n", laterOps)
	fmt.Printf("the second home drives a spawn, a bash moved to the background, the hook and a goal cron; only its %v rows are kept\n", spawnedOps)
	fmt.Printf("the third home drives a foreground bash under the %d line drain floor, on tofu drive --sift free, cheap marks and no jev call; only its %v rows are kept\n", konst.ShellDrainLinesFloor, siftedOps)
	timing := time.Now()
	rows, stopped, err := measure(first, more, sifted, runs)
	if err != nil {
		return false, err
	}
	fmt.Printf("timed %d ops in %s\n", len(rows), time.Since(timing).Round(100*time.Millisecond))
	path := filepath.Join(repo, filepath.FromSlash(baselineFile))
	if !update {
		return g.judge(path, rows, stopped)
	}
	written, err := json.MarshalIndent(baseline{Machine: g.machine, Date: time.Now().Format(time.RFC3339), Head: g.head, Runs: runs, Ops: rows}, "", "  ")
	if err != nil {
		return false, err
	}
	if failures := printRows(rows, nil, stopped); len(failures) > 0 {
		return true, fmt.Errorf("no baseline written: %s", strings.Join(failures, "; "))
	}
	fmt.Println("baseline", path)
	return false, os.WriteFile(path, append(written, '\n'), 0o644)
}

func (g gate) judge(path string, rows []measured, stopped error) (bool, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("%w; go run ./bench/gates/walls -update writes it", err)
	}
	var base baseline
	if err := json.Unmarshal(body, &base); err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	fmt.Printf("baseline %s: %s, %s, HEAD %s\n", path, base.Machine, base.Date, base.Head)
	failures := printRows(rows, base.Ops, stopped)
	if len(failures) > 0 {
		fmt.Printf("FAIL, these ops left their tier or were not recorded:\n  %s\n", strings.Join(failures, "\n  "))
		fmt.Println("Go back and make each op faster first, then run this gate again.")
		fmt.Println("When an op cannot be faster, because the app now does more there and the time is real, be realistic: tofu boardy move <ticket> done --bypass walls_stamp --why \"<op>: <why it cannot be faster>\", which records the reason on the ticket, then go run ./bench/gates/walls -update so the next ticket is measured against the new time.")
	}
	if !g.stamped {
		fmt.Println("no walls record: a stamp or a failure is written only for a run on the repository's own tree")
		return len(failures) > 0, nil
	}
	after, dirtyAfter, err := guard.HeadTree(g.repo)
	if err != nil || after != g.tree || len(g.dirty)+len(dirtyAfter) > 0 {
		fmt.Printf("no walls record: the run was not on a clean HEAD from start to end: tree %s then %s, uncommitted %q then %q\n", g.tree, after, g.dirty, dirtyAfter)
		return true, err
	}
	kind, lines := guard.WallsFailed, failures
	if len(failures) == 0 {
		kind, lines = guard.WallsStamps, []string{"head " + g.head, "date " + time.Now().Format(time.RFC3339)}
		for _, r := range rows {
			lines = append(lines, fmt.Sprintf("%s %s %d ms %s", r.Op, r.Name, r.OwnP50, r.Tier))
		}
	}
	record := guard.StampPath(g.repo, kind, g.tree)
	if err := os.MkdirAll(filepath.Dir(record), 0o755); err != nil {
		return false, err
	}
	fmt.Println(kind, record)
	return len(failures) > 0, os.WriteFile(record, []byte(strings.Join(append([]string{"tree " + g.tree}, lines...), "\n")+"\n"), 0o644)
}

func tierOf(ms int64) string {
	return tierLimits[slices.IndexFunc(tierLimits, func(limit tierLimit) bool { return ms < limit.under })].tier
}

func tierRank(tier string) int {
	return slices.IndexFunc(tierLimits, func(limit tierLimit) bool { return limit.tier == tier })
}

func rowsOf(own map[opName][]float64) []measured {
	var rows []measured
	for key, ms := range own {
		p50 := int64(math.Round(stat.Percentile(ms, 50)))
		rows = append(rows, measured{Op: key.op, Name: key.name, N: len(ms), OwnP50: p50, Tier: tierOf(p50)})
	}
	slices.SortFunc(rows, func(a, b measured) int { return cmp.Or(strings.Compare(a.Op, b.Op), strings.Compare(a.Name, b.Name)) })
	return rows
}

func printRows(rows, base []measured, stopped error) []string {
	var failures []string
	fmt.Println("| op | name | n | own p50 ms | tier | baseline | verdict |")
	fmt.Println("|---|---|---|---|---|---|---|")
	for _, now := range rows {
		was, verdict := "-", "new, not in the baseline"
		if base == nil {
			verdict = "baseline"
		}
		if at := slices.IndexFunc(base, func(b measured) bool { return b.Op == now.Op && b.Name == now.Name }); at >= 0 {
			was, verdict = fmt.Sprintf("%d %s", base[at].OwnP50, base[at].Tier), "pass"
			rose, noise := now.OwnP50-base[at].OwnP50, max(noiseFloorMS, base[at].OwnP50*noiseFloorPercent/100)
			switch {
			case tierRank(now.Tier) <= tierRank(base[at].Tier):
			case rose <= noise:
				verdict = fmt.Sprintf("pass, %s to %s within noise: +%d ms, floor %d", base[at].Tier, now.Tier, rose, noise)
			default:
				verdict = fmt.Sprintf("FAIL %s to %s, +%d ms", base[at].Tier, now.Tier, rose)
				failures = append(failures, fmt.Sprintf("%s %s: %d ms %s to %d ms %s", now.Op, now.Name, base[at].OwnP50, base[at].Tier, now.OwnP50, now.Tier))
			}
		}
		fmt.Printf("| %s | %s | %d | %d | %s | %s | %s |\n", now.Op, now.Name, now.N, now.OwnP50, now.Tier, was, verdict)
	}
	for _, b := range base {
		if !slices.ContainsFunc(rows, func(now measured) bool { return now.Op == b.Op && now.Name == b.Name }) {
			fmt.Printf("| %s | %s | 0 | - | - | %d %s | FAIL not produced |\n", b.Op, b.Name, b.OwnP50, b.Tier)
			failures = append(failures, fmt.Sprintf("%s %s: %d ms %s, now not produced", b.Op, b.Name, b.OwnP50, b.Tier))
		}
	}
	for _, op := range expectedOps {
		if !slices.ContainsFunc(rows, func(now measured) bool { return now.Op == string(op) }) {
			fmt.Printf("| %s | - | 0 | - | - | - | FAIL expected, not recorded |\n", op)
			failures = append(failures, fmt.Sprintf("%s: expected, and this run recorded no %s wall", op, op))
		}
	}
	if stopped != nil {
		failures = append(failures, "the run stopped early, so every n above is short: "+stopped.Error())
	}
	return failures
}

func build(src, work string) (string, error) {
	target, err := platformfs.ProductPackage(src)
	if err != nil {
		return "", err
	}
	exe := filepath.Join(work, "build", "tofu")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	cmd := exec.Command("go", "build", "-trimpath", "-p", childProcs, "-o", exe, target)
	cmd.Dir, cmd.Env = src, append(os.Environ(), "GOMAXPROCS="+childProcs)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("go build %s in %s: %v: %s", target, src, err, out)
	}
	return exe, nil
}

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if exited := (*exec.ExitError)(nil); errors.As(err, &exited) {
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), exited.Stderr)
	}
	return strings.TrimSpace(string(out)), err
}
