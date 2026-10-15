package main

import (
	"bufio"
	"cmp"
	"context"
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

type treeUsage struct {
	CPUMS       int64  `json:"cpu_ms"`
	PeakTreeRSS uint64 `json:"peak_tree_rss_bytes"`
	PeakCommit  uint64 `json:"peak_commit_bytes"`
	Processes   int    `json:"processes"`
	LeftRunning int    `json:"left_running_at_exit"`
}

type runDirs struct {
	repo, goDir, sessions string
}

type arm struct {
	Name, Tool, AskedModel, Posture string
	argv                            func(d runDirs, prompt string) []string
}

func tofuArm(name, model, wire string) arm {
	return arm{Name: name, Tool: "tofu", AskedModel: model, Posture: "--wire " + wire + " --gate shadow --no-instructions, real home for the login, scratch project folder",
		argv: func(d runDirs, prompt string) []string {
			return []string{"tofu", "run", "--dir", d.repo, "--model", model, "--wire", wire, "--effort", "medium", "--gate", "shadow", "--no-instructions", prompt}
		}}
}

var arms = []arm{
	tofuArm("tofu-sonnet", "claude-sub/claude-sonnet-5-5", "anthropic"),
	{Name: "claude-sonnet", Tool: "claude", AskedModel: "claude-sonnet-5-5", Posture: "--permission-mode bypassPermissions --safe-mode",
		argv: func(_ runDirs, prompt string) []string {
			return []string{"claude", "-p", prompt, "--model", "claude-sonnet-5-5", "--effort", "medium", "--output-format", "stream-json", "--verbose", "--permission-mode", "bypassPermissions", "--safe-mode"}
		}},
	{Name: "omp-sonnet", Tool: "omp", AskedModel: "anthropic/claude-sonnet-5-5", Posture: "--auto-approve --no-rules --no-skills --no-extensions, foreign config providers off by --config overlay",
		argv: func(d runDirs, prompt string) []string {
			return []string{"omp", "-p", "--mode", "json", "--model", "anthropic/claude-sonnet-5-5", "--thinking", "medium", "--auto-approve", "--no-rules", "--no-skills", "--no-extensions", "--no-title",
				"--config", filepath.Join(here(), "testdata", "omp-overlay.yml"), "--session-dir", d.sessions, "--cwd", d.repo, prompt}
		}},
	tofuArm("tofu-gpt", "codex-sub/gpt-5.6-sol", "codex"),
	{Name: "codex-gpt", Tool: "codex", AskedModel: "gpt-5.6-sol", Posture: "--sandbox workspace-write with the Go cache added, --ignore-rules",
		argv: func(d runDirs, prompt string) []string {
			return []string{"codex", "exec", prompt, "-m", "gpt-5.6-sol", "-c", `model="gpt-5.6-sol"`, "-c", `model_reasoning_effort="medium"`, "--json", "--sandbox", "workspace-write",
				"--add-dir", d.goDir, "--skip-git-repo-check", "--ignore-rules", "-C", d.repo}
		}},
}

type row struct {
	Task         string        `json:"task,omitempty"`
	Arm          string        `json:"arm"`
	Rep          int           `json:"rep"`
	Setting      string        `json:"setting,omitempty"`
	Batch        string        `json:"batch,omitempty"`
	Tool         string        `json:"tool"`
	Version      string        `json:"version"`
	Commit       string        `json:"commit,omitempty"`
	AskedModel   string        `json:"asked_model"`
	Model        string        `json:"model"`
	Models       []string      `json:"models"`
	Credential   string        `json:"credential"`
	Posture      string        `json:"posture"`
	Date         string        `json:"date"`
	Machine      string        `json:"machine"`
	ExitCode     int           `json:"exit_code"`
	WallMS       int64         `json:"wall_ms"`
	Tree         treeUsage     `json:"tree"`
	Tokens       tokens        `json:"tokens"`
	Requests     int           `json:"requests"`
	Auxiliary    int           `json:"auxiliary_requests,omitempty"`
	Calls        []call        `json:"calls"`
	Agents       int           `json:"agents,omitempty"`
	Timed        []requestTime `json:"timed_requests,omitempty"`
	ThinkingSent string        `json:"thinking_sent,omitempty"`
	Retried      int           `json:"retried"`
	Score        score         `json:"score"`
	Notes        []string      `json:"notes,omitempty"`
	Work         string        `json:"work,omitempty"`
}

func main() {
	if len(os.Args) < 2 {
		fail(errors.New("usage: rivals check [-task <name>] [-prompt <file>] | run [-task <name>] -arm <name> -rep <n> -slot <dir> | reparse [-batch <dir>] [-task <name>] | report | parity -batch <dir> [-task <name>]"))
	}
	switch os.Args[1] {
	case "check":
		fail(check(os.Args[2:]))
	case "run":
		fail(runArm(os.Args[2:]))
	case "reparse":
		fail(reparse(os.Args[2:]))
	case "report":
		fail(writeReport(os.Args[2:]))
	case "parity":
		fail(printParity(os.Args[2:]))
	default:
		fail(fmt.Errorf("unknown verb %q", os.Args[1]))
	}
}

func fail(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "rivals:", err)
		os.Exit(1)
	}
}

func here() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Dir(file)
}

func check(args []string) error {
	flags := flag.NewFlagSet("check", flag.ContinueOnError)
	work := flags.String("work", filepath.Join(os.TempDir(), "tofu-rivals"), "scratch root")
	name := flags.String("task", "", "task folder under testdata/tasks; empty is the first task in testdata")
	promptFile := flags.String("prompt", "", "check this prompt for leakage instead of the task's own")
	if err := flags.Parse(args); err != nil {
		return err
	}
	t, _, err := unleakedTask(*name, *promptFile)
	if err != nil {
		return err
	}
	for _, withReference := range []bool{false, true} {
		dirs, err := stage(filepath.Join(*work, fmt.Sprintf("check-%s-reference-%t", cmp.Or(t.Name, "first"), withReference)), t, withReference)
		if err != nil {
			return err
		}
		label := "untouched seed"
		if withReference {
			label = "seed plus reference solution"
		}
		fmt.Println(label + ":")
		fmt.Print(scoreRepo(dirs.repo, goEnv(dirs), t).text())
	}
	return nil
}

func stage(dir string, t task, withReference bool) (runDirs, error) {
	if err := os.RemoveAll(dir); err != nil {
		return runDirs{}, err
	}
	dirs := runDirs{repo: filepath.Join(dir, "repo"), goDir: filepath.Join(dir, "go"), sessions: filepath.Join(dir, "sessions")}
	sources := []string{filepath.Join(t.dir(), "seed")}
	if withReference {
		sources = append(sources, filepath.Join(t.dir(), "reference"))
	}
	for _, made := range []string{dirs.repo, filepath.Join(dirs.goDir, "cache"), filepath.Join(dirs.goDir, "tmp"), dirs.sessions} {
		if err := os.MkdirAll(made, 0o755); err != nil {
			return runDirs{}, err
		}
	}
	for _, source := range sources {
		if err := copyTree(source, dirs.repo); err != nil {
			return runDirs{}, err
		}
	}
	if t.Sea != nil {
		if err := writeSea(*t.Sea, dirs.repo); err != nil {
			return runDirs{}, err
		}
	}
	for _, git := range [][]string{{"init", "-q"}, {"config", "core.autocrlf", "false"}, {"add", "-A"},
		{"-c", "user.name=bench", "-c", "user.email=bench@example.invalid", "commit", "-q", "-m", "seed"}} {
		if out, err := exec.Command("git", append([]string{"-C", dirs.repo}, git...)...).CombinedOutput(); err != nil {
			return runDirs{}, fmt.Errorf("git %v: %v: %s", git, err, out)
		}
	}
	return dirs, nil
}

func goEnv(d runDirs) []string {
	keep := slices.DeleteFunc(os.Environ(), func(kv string) bool {
		name := strings.ToUpper(strings.SplitN(kv, "=", 2)[0])
		return strings.HasPrefix(name, "CLAUDE") || strings.HasPrefix(name, "OPENROUTER") || strings.HasSuffix(name, "_API_KEY") ||
			strings.HasPrefix(name, "GOCACHE") || strings.HasPrefix(name, "GOTMPDIR") || strings.HasPrefix(name, "GOFLAGS")
	})
	return append(keep, "GOCACHE="+filepath.Join(d.goDir, "cache"), "GOTMPDIR="+filepath.Join(d.goDir, "tmp"), "GOFLAGS=-p=2")
}

func runArm(args []string) error {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	name := flags.String("arm", "", "arm name")
	rep := flags.Int("rep", 0, "repeat number")
	slot := flags.String("slot", "", "directory taken with mkdir so only one tool runs on the machine")
	work := flags.String("work", filepath.Join(os.TempDir(), "tofu-rivals"), "scratch root")
	limit := flags.Duration("limit", 20*time.Minute, "wall clock cap")
	patience := flags.Duration("patience", 30*time.Minute, "how long to wait for the slot")
	setting := flags.String("setting", "", "the build or setting under test: shipped, rival, or summarized, tofu's thinkingSummary as it resolves with no file in the measured repo")
	batch := flags.String("batch", "", "folder under runs for this batch of rows, so a rerun keeps the rows before it")
	taskName := flags.String("task", "", "task folder under testdata/tasks; empty is the first task in testdata")
	if err := flags.Parse(args); err != nil {
		return err
	}
	t, prompt, err := unleakedTask(*taskName, "")
	if err != nil {
		return err
	}
	index := slices.IndexFunc(arms, func(a arm) bool { return a.Name == *name })
	if index < 0 || *rep < 1 || *slot == "" {
		return errors.New("run needs -arm, -rep and -slot")
	}
	switch *setting {
	case "", settingShipped, settingRival, thinkingSummarized:
	case thinkingOmitted:
		return errors.New("-setting omitted is refused: tofu run takes thinkingSummary only from .tofu/settings.json in the measured repo or from the global settings, a file in the repo is part of what the run sees, and tofu run has no flag for it yet")
	default:
		return fmt.Errorf("-setting is %s, %s or %s, not %q", settingShipped, settingRival, thinkingSummarized, *setting)
	}
	chosen := arms[index]
	for waited := time.Duration(0); os.Mkdir(*slot, 0o755) != nil; waited += 15 * time.Second {
		if waited >= *patience {
			return fmt.Errorf("the slot %s was held for %s, another tool is running", *slot, waited)
		}
		time.Sleep(15 * time.Second)
	}
	defer func() { _ = os.Remove(*slot) }()
	r := row{Task: t.Name, Arm: chosen.Name, Rep: *rep, Setting: *setting, Batch: *batch, Tool: chosen.Tool, AskedModel: chosen.AskedModel, Posture: chosen.Posture,
		Date: time.Now().Format("2006-01-02 15:04"), Machine: fmt.Sprintf("%s/%s, %d logical CPUs", runtime.GOOS, runtime.GOARCH, runtime.NumCPU())}
	r.Work = filepath.Join(*work, r.name())
	dirs, err := stage(r.Work, t, false)
	if err != nil {
		return err
	}
	env := goEnv(dirs)
	if chosen.Tool == "tofu" {
		r.Version, r.Commit = tofuBuild(toolOutput(dirs.repo, env, chosen.Tool, "version"))
		if *setting == thinkingSummarized {
			resolved := toolOutput(dirs.repo, env, "tofu", "settings", "get", "thinkingSummary")
			if resolved != thinkingSummarized {
				return fmt.Errorf("thinkingSummary resolves to %q in the run repo, not %s, and the bench writes no settings file", resolved, thinkingSummarized)
			}
			r.Notes = append(r.Notes, "setting: thinkingSummary "+resolved+", resolved with no settings file in the repo")
		}
	} else {
		r.Version = firstLine(toolOutput(dirs.repo, env, chosen.Tool, "--version"))
	}
	lines, stderr, err := drive(chosen, dirs, env, prompt, *limit, &r)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(r.Work, "stdout.txt"), []byte(joinLines(lines)), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(r.Work, "stderr.txt"), []byte(stderr), 0o644); err != nil {
		return err
	}
	r.Score = scoreRepo(dirs.repo, env, t)
	return save(withTranscript(r, parse(chosen, lines, dirs, env)))
}

func withTranscript(r row, t transcript) row {
	r.Model, r.Models, r.Credential, r.Requests, r.Tokens, r.Calls, r.Timed, r.ThinkingSent, r.Agents, r.Auxiliary = t.model, t.models, t.credential, t.requests, t.tokens, t.calls, t.timed, t.thinkingSent, t.agents, t.auxiliary
	r.Notes = append(slices.DeleteFunc(r.Notes, func(note string) bool {
		return !strings.HasPrefix(note, "stopped at") && !strings.HasPrefix(note, "wait:") && !strings.HasPrefix(note, "setting:")
	}), t.notes...)
	r.Retried = retried(t.calls)
	return r
}

func (r row) name() string {
	parts := slices.DeleteFunc([]string{r.Task, r.Arm, r.Setting, strconv.Itoa(r.Rep)}, func(part string) bool { return part == "" })
	return strings.Join(parts, "-")
}

func save(r row) error {
	body, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(here(), "runs", r.Batch, r.Task, r.name()+".json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	fmt.Printf("%s: %s %s, exit %d, %.1f s wall, %.1f s CPU, %s, %d requests, %d calls, %d sub-agents, out %d, reasoning %d, thinking sent %s, hidden %d of %d, rubric %d of %d\n", r.name(), r.Tool, r.build(), r.ExitCode, float64(r.WallMS)/1000,
		float64(r.Tree.CPUMS)/1000, r.Model, r.Requests, len(r.Calls), r.Agents, r.Tokens.Out, r.Tokens.Reasoning, cmp.Or(r.ThinkingSent, "not read"),
		r.Score.hiddenPassed(), len(r.Score.Hidden), r.Score.rubricPassed(), len(r.Score.Rubric))
	return os.WriteFile(path, append(body, '\n'), 0o644)
}

func reparse(args []string) error {
	flags := flag.NewFlagSet("reparse", flag.ContinueOnError)
	work := flags.String("work", filepath.Join(os.TempDir(), "tofu-rivals"), "scratch root")
	taskName := flags.String("task", "", "reparse only this task's rows; empty is the first task")
	batch := flags.String("batch", "", "folder under runs that holds the rows")
	if err := flags.Parse(args); err != nil {
		return err
	}
	paths, err := filepath.Glob(filepath.Join(here(), "runs", *batch, *taskName, "*.json"))
	if err != nil {
		return err
	}
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var r row
		if err := json.Unmarshal(body, &r); err != nil {
			return err
		}
		chosen := arms[slices.IndexFunc(arms, func(a arm) bool { return a.Name == r.Arm })]
		dir := cmp.Or(r.Work, filepath.Join(*work, r.name()))
		raw, err := os.ReadFile(filepath.Join(dir, "stdout.txt"))
		if err != nil {
			return err
		}
		var lines []stamped
		for _, line := range strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n") {
			ms, text, _ := strings.Cut(line, "\t")
			at, _ := strconv.ParseInt(ms, 10, 64)
			lines = append(lines, stamped{ms: at, text: text})
		}
		dirs := runDirs{repo: filepath.Join(dir, "repo"), goDir: filepath.Join(dir, "go"), sessions: filepath.Join(dir, "sessions")}
		if err := os.MkdirAll(dirs.repo, 0o755); err != nil {
			return err
		}
		if err := save(withTranscript(r, parse(chosen, lines, dirs, goEnv(dirs)))); err != nil {
			return err
		}
	}
	return nil
}

type stamped struct {
	ms   int64
	text string
}

func drive(chosen arm, dirs runDirs, env []string, prompt string, limit time.Duration, r *row) ([]stamped, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	argv := chosen.argv(dirs, prompt)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir, cmd.Env = dirs.repo, env
	var stderr strings.Builder
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, "", err
	}
	started := time.Now()
	measured, err := startTree(cmd)
	if err != nil {
		return nil, "", err
	}
	var lines []stamped
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 1<<20), 64<<20)
	for scanner.Scan() {
		lines = append(lines, stamped{ms: time.Since(started).Milliseconds(), text: scanner.Text()})
	}
	waitErr := cmd.Wait()
	r.WallMS = time.Since(started).Milliseconds()
	r.Tree = measured.finish(cmd.ProcessState)
	r.ExitCode = cmd.ProcessState.ExitCode()
	if ctx.Err() != nil {
		r.Notes = append(r.Notes, fmt.Sprintf("stopped at the %s wall clock cap", limit))
	}
	if waitErr != nil && r.ExitCode == 0 {
		r.Notes = append(r.Notes, "wait: "+waitErr.Error())
	}
	return lines, stderr.String(), nil
}

func toolOutput(dir string, env []string, name string, args ...string) string {
	cmd := exec.Command(name, args...)
	cmd.Dir, cmd.Env = dir, env
	out, err := cmd.CombinedOutput()
	if err != nil {
		return strings.TrimSpace(string(out)) + " (" + err.Error() + ")"
	}
	return strings.TrimSpace(string(out))
}

func tofuBuild(said string) (string, string) {
	fields := map[string]string{}
	for _, line := range strings.Split(said, "\n") {
		name, value, _ := strings.Cut(strings.TrimSpace(line), " ")
		fields[name] = strings.TrimSpace(value)
	}
	return fields["version"], fields["commit"]
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(text, "\n")
	return strings.TrimSpace(line)
}

func joinLines(lines []stamped) string {
	var b strings.Builder
	for _, line := range lines {
		fmt.Fprintf(&b, "%d\t%s\n", line.ms, line.text)
	}
	return b.String()
}
