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
)

const (
	model            = "claude-sub/claude-sonnet-5"
	mockLoginCode    = "mock-login-code"
	buildProcs       = "4"
	defaultCeiling   = 60000
	stepsAfterScript = 5
)

type arm struct {
	name     string
	settings []string
}

var arms = []arm{
	{name: "episode carry"},
	{name: "working-state ask", settings: []string{"settings", "set", "episodes", "off"}},
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "forkcache:", err)
		os.Exit(1)
	}
}

func run() error {
	exe := flag.String("tofu", "", "the tofu under measurement; built from this repository into -work when empty")
	work := flag.String("work", "", "a short folder, made fresh, that the build, the homes and the mocked projects go under")
	ceiling := flag.Int("ceiling", defaultCeiling, "the --context-ceiling every run is given, low enough that a recorded turn crosses its fork target")
	flag.Parse()
	if *work == "" {
		return errors.New("-work names a short folder that does not exist yet")
	}
	if _, err := os.Stat(*work); err == nil {
		return fmt.Errorf("%s exists; every run writes a fresh one", *work)
	}
	root, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return fmt.Errorf("forkcache reads its corpus from the repository it runs in: %w", err)
	}
	repo := strings.TrimSpace(string(root))
	scripts, err := readCorpus(repo)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(*work, 0o755); err != nil {
		return err
	}
	if *exe == "" {
		if *exe, err = build(repo, *work); err != nil {
			return err
		}
	}
	header := conditions(*exe, repo, *ceiling)
	var runs []measured
	for _, a := range arms {
		for _, played := range scripts {
			ran, err := replay(*exe, filepath.Join(*work, strconv.Itoa(len(runs)+1)), played, a, *ceiling)
			if err != nil {
				return fmt.Errorf("%s on %s: %w", a.name, played.name, err)
			}
			runs = append(runs, ran)
		}
	}
	var compacts []compactRow
	for _, played := range scripts {
		row, err := compactOnce(*exe, filepath.Join(*work, strconv.Itoa(len(runs)+len(compacts)+1)), played)
		if err != nil {
			return fmt.Errorf("%s on %s: %w", compactArm, played.name, err)
		}
		compacts = append(compacts, row)
	}
	fmt.Print(header + report(runs) + compactReport(compacts) + flipReport(runs, compacts))
	past := 0
	for _, r := range runs {
		past += r.past
	}
	for _, c := range compacts {
		past += c.past
	}
	if past > 0 {
		return fmt.Errorf("%d requests reach past the %d-block lookback", past, lookbackBlocks)
	}
	return nil
}

func build(repo, work string) (string, error) {
	exe := filepath.Join(work, "tofu.exe")
	cmd := exec.Command("go", "build", "-trimpath", "-p", buildProcs, "-o", exe, "./cmd/tofu")
	cmd.Dir, cmd.Env = repo, append(os.Environ(), "GOMAXPROCS="+buildProcs)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("go build ./cmd/tofu: %v: %s", err, out)
	}
	return exe, nil
}

func freshEnv(home string, extra ...string) []string {
	env := slices.DeleteFunc(os.Environ(), func(kv string) bool {
		name := strings.ToUpper(strings.SplitN(kv, "=", 2)[0])
		return name == "HOME" || name == "USERPROFILE" || strings.HasPrefix(name, "TOFU_") || strings.HasPrefix(name, "CLAUDE") ||
			strings.HasPrefix(name, "OPENROUTER") || strings.HasSuffix(name, "_API_KEY")
	})
	return append(append(env, "HOME="+home, "USERPROFILE="+home), extra...)
}

func tofu(exe, dir string, env []string, stdin string, args ...string) (string, error) {
	cmd := exec.Command(exe, args...)
	cmd.Dir, cmd.Env, cmd.Stdin = dir, env, strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("tofu %s: %v: %s", args[0], err, out[max(0, len(out)-4000):])
	}
	return string(out), nil
}

type staged struct {
	stood    *stand
	env      []string
	dir      string
	task     string
	started  time.Time
	timedFor string
}

func stage(exe, work string, played script, settings []string) (staged, error) {
	home, dir := filepath.Join(work, "home"), filepath.Join(work, "project")
	for _, made := range []string{home, filepath.Join(dir, "mock")} {
		if err := os.MkdirAll(made, 0o755); err != nil {
			return staged{}, err
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("mock project\n"), 0o644); err != nil {
		return staged{}, err
	}
	for _, git := range [][]string{{"init", "-q"}, {"add", "-A"}, {"-c", "user.name=bench", "-c", "user.email=bench@example.invalid", "commit", "-q", "-m", "seed"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, git...)...).CombinedOutput(); err != nil {
			return staged{}, fmt.Errorf("git %v: %v: %s", git, err, out)
		}
	}
	stood := startStand(dir, played)
	env := freshEnv(home, stood.env()...)
	setup := [][]string{{"login", "llm", "claude-sub", "--paste"}, {"models", "bind", "classifier", "typesafe/jev-latest"}, {"models", "bind", "orchestrator", model}}
	if settings != nil {
		setup = append(setup, settings)
	}
	for _, args := range setup {
		if _, err := tofu(exe, dir, env, mockLoginCode+"\n", args...); err != nil {
			stood.Close()
			return staged{}, err
		}
	}
	task := "MOCK task " + strings.Repeat("z", max(0, played.taskBytes-10))
	return staged{stood: stood, env: env, dir: dir, task: task, started: time.Now(), timedFor: played.name}, nil
}

func (s staged) kept(work, armName string) error {
	s.stood.held.Lock()
	defer s.stood.held.Unlock()
	if s.stood.failed != nil {
		return fmt.Errorf("the stand-in could not stage a mocked call: %w", s.stood.failed)
	}
	for at, e := range s.stood.sent {
		if err := os.WriteFile(filepath.Join(work, fmt.Sprintf("request-%03d.json", at)), []byte(e.body), 0o644); err != nil {
			return err
		}
	}
	fmt.Fprintf(os.Stderr, "timing, not measured: %s on %s: %d requests in %s\n", armName, s.timedFor, len(s.stood.sent), time.Since(s.started).Round(time.Second))
	return nil
}

func replay(exe, work string, played script, a arm, ceiling int) (measured, error) {
	s, err := stage(exe, work, played, a.settings)
	if err != nil {
		return measured{}, err
	}
	defer s.stood.Close()
	_, err = tofu(exe, s.dir, s.env, "", "run", "--dir", s.dir, "--model", model, "--no-gate", "--sift", "free",
		"--context-ceiling", strconv.Itoa(ceiling), "--max-steps", strconv.Itoa(len(played.steps)+stepsAfterScript), s.task)
	if err != nil {
		return measured{}, err
	}
	listed, err := tofu(exe, s.dir, s.env, "", "session", "list", "--json")
	if err != nil {
		return measured{}, err
	}
	if err := s.kept(work, a.name); err != nil {
		return measured{}, err
	}
	return measure(a.name, played, s.stood.sent, s.stood.lookups, listed), nil
}

func conditions(exe, repo string, ceiling int) string {
	version, _ := exec.Command(exe, "version").Output()
	commit, _ := exec.Command("git", "-C", repo, "rev-parse", "--short", "HEAD").Output()
	return fmt.Sprintf(`bench/forkcache, fork cost and quality on mocked recorded sessions

commit      %s
tofu        %s
date        %s
machine     %s/%s, %d cpu
wire        anthropic messages to a local stand-in that bills by Anthropic's prompt cache rules; no model, no network
project     billed as W:\mock\project in every request, so the length of -work moves no count
credential  mock claude-sub login against the stand-in, so a subscription credential kind
head        subscription: the stand-in's token carries the oauth marker, so every request sends the billing and identity blocks
model       %s, build id the stand-in reported: %s
ceiling     --context-ceiling %d on every tofu run; the compact arm runs under tofu serve --stdio at its own context budget
weights     input %.2f, cache read %.2f, write 5m %.2f, write 1h %.2f, in base input tokens
key         tools, system, then messages; tool_choice is not in the key, so a switch to none keeps the message cache
checked     against a real claude-sub run, claude-haiku-5-5 on 2026-11-01, trace rapid-sable-finch#ub4fl.1: step #640793 read 17098 wrote 1463, then the ask #9e8c41 sent with tool_choice none read 18561
lookback    %d blocks back from each cache breakpoint; a row whose reach is past it reads fail and the run exits nonzero
warm-up     none: no request is discarded

`, strings.TrimSpace(string(commit)), strings.Join(strings.Fields(string(version)), " "), time.Now().Format(time.DateOnly),
		runtime.GOOS, runtime.GOARCH, runtime.NumCPU(), model, reportedModel, ceiling, inputWeight, readWeight, write5mWeight, write1hWeight, lookbackBlocks)
}
