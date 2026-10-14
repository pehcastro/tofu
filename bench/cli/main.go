package main

import (
	"bytes"
	"cmp"
	"encoding/json"
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

const (
	deadProxy = "http://127.0.0.1:9"
	benchDir  = "bench/cli"
)

type row struct {
	Command   string
	WallMs    float64
	WallP90Ms float64
	CPUMs     float64
	RSSMB     float64
}

type pass struct {
	Name     string
	Against  string
	Note     string
	Version  string
	Commit   string
	Recorded time.Time
	Platform string
	Method   string
	Runs     int
	InitMs   float64
	InitMB   float64
	Rows     []row
}

func commands() [][]string {
	return [][]string{
		{"version"}, {"help"}, {"docs"}, {"changelog"}, {"settings"}, {"memory"},
		{"agents"}, {"models"}, {"library"}, {"hooks"}, {"context"}, {"scratch"},
		{"learn"}, {"usage"}, {"doctor"}, {"frame"}, {"reload"}, {"migrate", "--dry-run"},
		{"preview"}, {"serve", "--schema"}, {"drive"}, {"update", "--check"}, {"browser"},
		{"session", "list"}, {"shells", "list"}, {"rules", "list"}, {"lint", "comments"},
		{"boardy"}, {"why", "--last"}, {"label"}, {"replay"}, {"undo"}, {"check"},
		{"judge"}, {"sift"}, {"run"}, {"logout"}, {"login", "--status"},
	}
}

func main() {
	name := flag.String("pass", "", "the pass name, v0 for the baseline")
	against := flag.String("against", "v0", "the earlier pass this one is compared with")
	note := flag.String("note", "", "what changed in this pass")
	runs := flag.Int("n", 7, "runs per command")
	modfile := flag.String("modfile", "", "build against this go.mod instead of the tree's, to measure an earlier dependency set")
	renderOnly := flag.Bool("render", false, "rewrite passes.md from the recorded passes and measured.json without running a pass")
	flag.Parse()
	if *renderOnly {
		if err := render(); err != nil {
			fmt.Fprintln(os.Stderr, "bench/cli:", err)
			os.Exit(1)
		}
		return
	}
	if *name == "" {
		fmt.Fprintln(os.Stderr, "bench/cli: -pass is required")
		os.Exit(2)
	}
	if err := record(pass{Name: *name, Against: *against, Note: *note, Runs: *runs, Recorded: time.Now().UTC(), Platform: runtime.GOOS + "/" + runtime.GOARCH, Method: cpuMethod}, *modfile); err != nil {
		fmt.Fprintln(os.Stderr, "bench/cli:", err)
		os.Exit(1)
	}
}

func record(result pass, modfile string) error {
	work, err := os.MkdirTemp("", "benchcli")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(work) }()
	binary := filepath.Join(work, "tofu")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-modfile="+cmp.Or(modfile, "go.mod"), "-o", binary, "./cmd/tofu")
	build.Env = append(os.Environ(), "GOMAXPROCS=2")
	build.Stdout, build.Stderr = os.Stderr, os.Stderr
	if err := build.Run(); err != nil {
		return fmt.Errorf("building tofu: %w", err)
	}
	meter := newMeter()
	var initMs, initBytes []float64
	for range result.Runs {
		r, err := execute(meter, binary, work, []string{"version"}, "inittrace=1")
		if err != nil {
			return err
		}
		for line := range strings.Lines(r.stdout) {
			if commit, ok := strings.CutPrefix(line, "commit"); ok {
				result.Commit = strings.TrimSpace(commit)
			}
			if version, ok := strings.CutPrefix(line, "version"); ok {
				result.Version = strings.TrimSpace(version)
			}
		}
		ms, bytes := initCost(r.stderr)
		initMs, initBytes = append(initMs, ms), append(initBytes, bytes)
	}
	result.InitMs, result.InitMB = rank(initMs, 50), rank(initBytes, 50)/1e6
	for _, command := range commands() {
		var wall, cpu, rss []float64
		for range result.Runs {
			r, err := execute(meter, binary, work, command, "")
			if err != nil {
				return fmt.Errorf("%s: %w", strings.Join(command, " "), err)
			}
			wall, cpu, rss = append(wall, ms(r.elapsed)), append(cpu, ms(r.used.cpu)), append(rss, float64(r.used.peakRSS)/1e6)
		}
		result.Rows = append(result.Rows, row{Command: strings.Join(command, " "), WallMs: rank(wall, 50), WallP90Ms: rank(wall, 90), CPUMs: rank(cpu, 50), RSSMB: rank(rss, 50)})
		fmt.Fprintf(os.Stderr, "%-18s cpu %6.1f ms  wall %6.1f ms\n", strings.Join(command, " "), rank(cpu, 50), rank(wall, 50))
	}
	if err := os.MkdirAll(filepath.Join(benchDir, "passes"), 0o755); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(benchDir, "passes", result.Name+".json"), append(encoded, '\n'), 0o644); err != nil {
		return err
	}
	return render()
}

type usage struct {
	cpu     time.Duration
	peakRSS uint64
}

type run struct {
	elapsed        time.Duration
	used           usage
	stdout, stderr string
}

func execute(m meter, binary, work string, command []string, godebug string) (run, error) {
	home, err := os.MkdirTemp(work, "home")
	if err != nil {
		return run{}, err
	}
	defer func() { _ = os.RemoveAll(home) }()
	replaced := map[string]string{
		"HOME": home, "USERPROFILE": home, "APPDATA": filepath.Join(home, "AppData"),
		"XDG_CONFIG_HOME": filepath.Join(home, ".config"), "GOMAXPROCS": "2", "GODEBUG": godebug,
		"HTTPS_PROXY": deadProxy, "HTTP_PROXY": deadProxy, "NO_PROXY": "",
	}
	env := slices.DeleteFunc(os.Environ(), func(entry string) bool {
		key, _, _ := strings.Cut(entry, "=")
		_, ok := replaced[strings.ToUpper(key)]
		return ok
	})
	for key, value := range replaced {
		env = append(env, key+"="+value)
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(binary, command...)
	cmd.Dir, cmd.Env, cmd.Stdout, cmd.Stderr = home, env, &stdout, &stderr
	start := time.Now()
	if err := cmd.Start(); err != nil {
		return run{}, err
	}
	used, err := m.watch(cmd.Process.Pid)
	if err != nil {
		return run{}, err
	}
	var exit *exec.ExitError
	if err := cmd.Wait(); err != nil && !errors.As(err, &exit) {
		return run{}, err
	}
	return run{elapsed: time.Since(start), used: used(cmd.ProcessState), stdout: stdout.String(), stderr: stderr.String()}, nil
}

func initCost(trace string) (float64, float64) {
	var clock, allocated float64
	for line := range strings.Lines(trace) {
		words := strings.Fields(line)
		if len(words) < 8 || words[0] != "init" {
			continue
		}
		ms, _ := strconv.ParseFloat(words[4], 64)
		bytes, _ := strconv.ParseFloat(words[7], 64)
		clock, allocated = clock+ms, allocated+bytes
	}
	return clock, allocated
}

func ms(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000
}

func rank(values []float64, percent int) float64 {
	sorted := slices.Sorted(slices.Values(values))
	return sorted[(len(sorted)*percent+99)/100-1]
}
