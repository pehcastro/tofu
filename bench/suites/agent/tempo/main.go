package main

import (
	"bufio"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
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
	armClaude  = "claude"
	askedModel = "opus"
	wire       = "claude -p --output-format stream-json --verbose, prompt on stdin"
	checkLimit = 5 * time.Minute
	checkTail  = 1500
)

var allowedTools = []string{"Read", "Write", "Edit", "Glob", "Grep", "Bash(bun:*)", "Bash(bunx:*)", "Bash(ls:*)", "Bash(cat:*)", "Bash(mkdir:*)",
	"Bash(curl:*)", "Bash(node:*)", "Bash(rm:*)", "Bash(sleep:*)", "Bash(kill:*)", "Bash(timeout:*)"}

type check struct {
	Passed bool   `json:"passed"`
	Tail   string `json:"tail"`
}

type row struct {
	Task       string     `json:"task"`
	Arm        string     `json:"arm"`
	Rep        int        `json:"rep"`
	Batch      string     `json:"batch"`
	AskedModel string     `json:"asked_model"`
	Wire       string     `json:"wire"`
	Posture    string     `json:"posture"`
	Machine    string     `json:"machine"`
	StartedAt  time.Time  `json:"started_at"`
	ExitCode   int        `json:"exit_code"`
	WallMS     int64      `json:"wall_ms"`
	Check      check      `json:"check"`
	Prediction prediction `json:"prediction"`
	Work       string     `json:"work"`
	Notes      []string   `json:"notes,omitempty"`
	measured
}

func main() {
	if len(os.Args) < 2 {
		fail(errors.New("usage: tempo run -task <id> -arm claude -rep <n> -predictions <dir> [-batch <name>] [-work <dir>] | reparse -batch <name> | report -batch <name>"))
	}
	switch os.Args[1] {
	case "run":
		fail(run(os.Args[2:]))
	case "reparse":
		fail(reparse(os.Args[2:]))
	case "report":
		fail(writeReport(os.Args[2:]))
	default:
		fail(fmt.Errorf("unknown verb %q", os.Args[1]))
	}
}

func fail(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "tempo:", err)
		os.Exit(1)
	}
}

func here() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Dir(file)
}

func (r row) name() string {
	return r.Task + "-" + r.Arm + "-" + strconv.Itoa(r.Rep)
}

func (r row) path() string {
	return filepath.Join(here(), "runs", r.Batch, r.Task, r.name()+".json")
}

func run(args []string) error {
	start := time.Now()
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	task := flags.String("task", "", "task folder under testdata/tasks")
	arm := flags.String("arm", "", "arm name, only claude")
	rep := flags.Int("rep", 0, "repeat number")
	predictions := flags.String("predictions", "", "folder holding <task>.json written before the run")
	batch := flags.String("batch", "", "folder under runs for the rows; empty is the predictions folder's name")
	work := flags.String("work", filepath.Join(os.TempDir(), "tofu-tempo"), "scratch root")
	limit := flags.Duration("limit", 3*time.Hour, "wall clock cap")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *arm != armClaude || *rep < 1 || *predictions == "" {
		return errors.New("run needs -arm claude, -rep 1 or more and -predictions")
	}
	dir, err := taskDir(*task)
	if err != nil {
		return err
	}
	promptBody, err := os.ReadFile(filepath.Join(dir, "prompt.md"))
	if err != nil {
		return err
	}
	common, err := os.ReadFile(filepath.Join(here(), "testdata", "tasks", "common.md"))
	if err != nil {
		return err
	}
	p, err := loadPrediction(*predictions, *task, promptBody, start)
	if err != nil {
		return err
	}
	if os.Getenv("TOFU_LIVE") == "" {
		return errors.New("run drives the claude command line on the owner's subscription; set TOFU_LIVE=1")
	}
	r := row{Task: *task, Arm: *arm, Rep: *rep, Batch: cmp.Or(*batch, filepath.Base(filepath.Clean(*predictions))), AskedModel: askedModel, Wire: wire,
		Posture:   "--effort medium --permission-mode acceptEdits --allowedTools " + strings.Join(allowedTools, " ") + ", env without CLAUDE*, OPENROUTER* and *_API_KEY, the owner's ~/.claude settings and hooks apply as in the lab",
		Machine:   fmt.Sprintf("%s/%s, %d logical CPUs", runtime.GOOS, runtime.GOARCH, runtime.NumCPU()),
		StartedAt: start, Prediction: p}
	if _, err := os.Stat(r.path()); err == nil {
		return fmt.Errorf("%s exists; pick another -rep or -batch", r.path())
	}
	r.Work = filepath.Join(*work, r.Batch, r.name())
	repo := filepath.Join(r.Work, "repo")
	if err := stage(repo, dir); err != nil {
		return err
	}
	env := strippedEnv()
	lines, stderr, err := drive(repo, env, string(promptBody)+string(common), *limit, &r)
	if err != nil {
		return err
	}
	var stdout strings.Builder
	for _, line := range lines {
		fmt.Fprintf(&stdout, "%d\t%s\n", line.ms, line.text)
	}
	if err := os.WriteFile(filepath.Join(r.Work, "stdout.txt"), []byte(stdout.String()), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(r.Work, "stderr.txt"), []byte(stderr), 0o644); err != nil {
		return err
	}
	if err := keepTranscript(r.Work, measureStream(lines).Session, &r); err != nil {
		return err
	}
	if r.Check, err = runCheck(repo, dir, env); err != nil {
		return err
	}
	if r.measured, err = measureWork(r.Work); err != nil {
		return err
	}
	return save(r)
}

func stage(repo, dir string) error {
	if err := os.RemoveAll(filepath.Dir(repo)); err != nil {
		return err
	}
	for _, source := range []string{filepath.Join(here(), "testdata", "starter"), filepath.Join(dir, "seed")} {
		if err := copyTree(source, repo); err != nil {
			return err
		}
	}
	install := exec.Command("bun", "install", "--frozen-lockfile")
	install.Dir = repo
	if out, err := install.CombinedOutput(); err != nil {
		return fmt.Errorf("bun install: %v: %s", err, out)
	}
	return nil
}

func copyTree(source, target string) error {
	if _, err := os.Stat(source); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return filepath.WalkDir(source, func(file string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(source, file)
		if err != nil {
			return err
		}
		body, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(filepath.Join(target, rel)), 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(target, rel), body, 0o644)
	})
}

func strippedEnv() []string {
	return slices.DeleteFunc(os.Environ(), func(kv string) bool {
		name := strings.ToUpper(strings.SplitN(kv, "=", 2)[0])
		return strings.HasPrefix(name, "CLAUDE") || strings.HasPrefix(name, "OPENROUTER") || strings.HasSuffix(name, "_API_KEY")
	})
}

func drive(repo string, env []string, prompt string, limit time.Duration, r *row) ([]stamped, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	argv := append([]string{"-p", "--model", askedModel, "--effort", "medium", "--output-format", "stream-json", "--verbose", "--permission-mode", "acceptEdits", "--allowedTools"}, allowedTools...)
	cmd := exec.CommandContext(ctx, "claude", argv...)
	cmd.Dir, cmd.Env, cmd.Stdin = repo, env, strings.NewReader(prompt)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, "", err
	}
	started := time.Now()
	if err := cmd.Start(); err != nil {
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
	r.ExitCode = cmd.ProcessState.ExitCode()
	if ctx.Err() != nil {
		r.Notes = append(r.Notes, fmt.Sprintf("stopped at the %s wall clock cap", limit))
	}
	if waitErr != nil && r.ExitCode == 0 {
		r.Notes = append(r.Notes, "wait: "+waitErr.Error())
	}
	return lines, stderr.String(), nil
}

func keepTranscript(work, session string, r *row) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	found, _ := filepath.Glob(filepath.Join(home, ".claude", "projects", "*", session+".jsonl"))
	var body []byte
	if session == "" || len(found) != 1 {
		r.Notes = append(r.Notes, fmt.Sprintf("session %q has %d transcripts under ~/.claude/projects, so active_ms is 0", session, len(found)))
	} else if body, err = os.ReadFile(found[0]); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(work, "transcript.jsonl"), body, 0o644)
}

func runCheck(repo, dir string, env []string) (check, error) {
	hidden := filepath.Join(repo, "check")
	if err := copyTree(filepath.Join(dir, "check.test.ts"), filepath.Join(hidden, "check.test.ts")); err != nil {
		return check{}, err
	}
	defer func() { _ = os.RemoveAll(hidden) }()
	ctx, cancel := context.WithTimeout(context.Background(), checkLimit)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bun", "test", "./check/check.test.ts")
	cmd.Dir, cmd.Env = repo, env
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		return check{}, err
	}
	tail := string(out)
	return check{Passed: err == nil, Tail: tail[max(0, len(tail)-checkTail):]}, nil
}

func save(r row) error {
	body, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(r.path()), 0o755); err != nil {
		return err
	}
	fmt.Printf("%s: exit %d, %.1f s wall, %.1f s active, %.1f s model, %s, %s, %d requests, out %d, check %t, predicted P50 %.1f s\n", r.name(), r.ExitCode,
		float64(r.WallMS)/1000, float64(r.ActiveMS)/1000, float64(r.ModelMS)/1000, r.Model, r.Credential, r.Requests, r.Tokens.Out, r.Check.Passed, r.Prediction.P50S)
	return os.WriteFile(r.path(), append(body, '\n'), 0o644)
}

func reparse(args []string) error {
	flags := flag.NewFlagSet("reparse", flag.ContinueOnError)
	batch := flags.String("batch", "", "folder under runs that holds the rows")
	if err := flags.Parse(args); err != nil {
		return err
	}
	rows, err := loadRows(*batch)
	if err != nil {
		return err
	}
	for _, r := range rows {
		if r.measured, err = measureWork(r.Work); err != nil {
			return err
		}
		if err := save(r); err != nil {
			return err
		}
	}
	return nil
}

func loadRows(batch string) ([]row, error) {
	if batch == "" {
		return nil, errors.New("-batch names a folder under runs")
	}
	paths, err := filepath.Glob(filepath.Join(here(), "runs", batch, "*", "*.json"))
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no rows under runs/%s", batch)
	}
	var rows []row
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var r row
		if err := json.Unmarshal(body, &r); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		rows = append(rows, r)
	}
	return rows, nil
}
