package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"strings"
	"time"

	"tofu/bench/stat"
	"tofu/internal/konst"
	"tofu/internal/turn"
	"tofu/internal/turn/tools"
)

type call struct {
	tool string
	args string
}

func calls() []call {
	return []call{
		{"read", `{"path":"src/net/http/server.go"}`},
		{"glob", `{"pattern":"**/*_test.go"}`},
		{"grep", `{"pattern":"ServeHTTP"}`},
		{"search", `{"pattern":"func \\(\\w+ \\*?\\w+\\) Close\\(\\) error"}`},
	}
}

func runner(repo, name string) (turn.Tool, error) {
	switch name {
	case "read":
		return turn.NewReadTool(repo)
	case "glob":
		return tools.NewGlob(repo)
	case "grep", "search":
		return tools.NewSearch(repo)
	}
	return nil, fmt.Errorf("no tool named %s", name)
}

func timed(repo string, c call) (time.Duration, string, error) {
	tool, err := runner(repo, c.tool)
	if err != nil {
		return 0, "", err
	}
	started := time.Now()
	result, err := tool.Run(context.Background(), json.RawMessage(c.args))
	return time.Since(started), result.Content, err
}

func once(repo string) error {
	var spent []string
	for _, c := range calls() {
		took, _, err := timed(repo, c)
		if err != nil {
			return err
		}
		spent = append(spent, fmt.Sprint(took.Microseconds()))
	}
	fmt.Println(strings.Join(spent, " "))
	return nil
}

func coldSamples(repo string, runs int) ([][]float64, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	samples := make([][]float64, len(calls()))
	for range runs {
		out, err := exec.Command(self, "-repo", repo, "-once").Output()
		if err != nil {
			return nil, fmt.Errorf("cold run: %w", err)
		}
		for i, field := range strings.Fields(string(out)) {
			var micros float64
			if _, err := fmt.Sscan(field, &micros); err != nil {
				return nil, err
			}
			samples[i] = append(samples[i], micros/1000)
		}
	}
	return samples, nil
}

func main() {
	repo := flag.String("repo", "", "the pinned checkout to run the tools over")
	single := flag.Bool("once", false, "run each call once and print its microseconds")
	cold := flag.Int("cold", 10, "fresh processes, one call per tool each")
	warm := flag.Int("warm", 20, "calls per tool in this process after one discarded")
	out := flag.String("out", "", "a folder to write each tool's result into")
	profile := flag.String("cpuprofile", "", "a file to write a cpu profile of the whole run into")
	flag.Parse()
	err := profiled(*profile, func() error { return run(*repo, *single, *cold, *warm, *out) })
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func profiled(into string, work func() error) error {
	if into == "" {
		return work()
	}
	file, err := os.Create(into)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	if err := pprof.StartCPUProfile(file); err != nil {
		return err
	}
	defer pprof.StopCPUProfile()
	return work()
}

func builtFrom() string {
	revision, dirty := "unstamped", ""
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return revision
	}
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value[:min(8, len(setting.Value))]
		case "vcs.modified":
			if setting.Value == "true" {
				dirty = " with uncommitted changes"
			}
		}
	}
	return revision + dirty
}

func run(repo string, single bool, cold, warm int, out string) error {
	if single {
		return once(repo)
	}
	pinned, err := exec.Command("git", "-C", repo, "describe", "--tags", "--exact-match").Output()
	if err != nil {
		return fmt.Errorf("the repository is not at a pinned tag: %w", err)
	}
	files, err := tools.ListPaths(repo)
	if err != nil {
		return err
	}
	coldMs, err := coldSamples(repo, cold)
	if err != nil {
		return err
	}
	fmt.Printf("tofu %s at %s, %s %s, %d files listed, GOMAXPROCS %d, %s, cold %d processes, warm %d calls\n",
		konst.Version, builtFrom(), filepath.Base(repo), strings.TrimSpace(string(pinned)),
		len(files), runtime.GOMAXPROCS(0), runtime.GOOS, cold, warm)
	fmt.Printf("%-7s %-5s %9s %9s %8s  %s\n", "tool", "state", "p50 ms", "p99 ms", "bytes", "sha256")
	for i, c := range calls() {
		_, content, err := timed(repo, c)
		if err != nil {
			return err
		}
		var warmMs []float64
		for range warm {
			took, _, err := timed(repo, c)
			if err != nil {
				return err
			}
			warmMs = append(warmMs, float64(took.Microseconds())/1000)
		}
		digest := fmt.Sprintf("%x", sha256.Sum256([]byte(content)))[:16]
		fmt.Printf("%-7s %-5s %9.1f %9.1f %8d  %s\n", c.tool, "cold", stat.Percentile(coldMs[i], 50), stat.Percentile(coldMs[i], 99), len(content), digest)
		fmt.Printf("%-7s %-5s %9.1f %9.1f %8d  %s\n", c.tool, "warm", stat.Percentile(warmMs, 50), stat.Percentile(warmMs, 99), len(content), digest)
		if out != "" {
			if err := os.WriteFile(filepath.Join(out, c.tool+".txt"), []byte(content), 0o644); err != nil {
				return err
			}
		}
	}
	return nil
}
