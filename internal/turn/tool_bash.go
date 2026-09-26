package turn

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/search"
	"tofu/internal/shell"
)

type BashTool struct {
	root   Root
	choice shell.Choice
	probe  *toolchainProbe
}

type shellRegistryKey struct{}

func WithShellRegistry(ctx context.Context, registry *shell.Registry) context.Context {
	if registry == nil {
		return ctx
	}
	return context.WithValue(ctx, shellRegistryKey{}, registry)
}

func shellRegistryFrom(ctx context.Context) *shell.Registry {
	registry, _ := ctx.Value(shellRegistryKey{}).(*shell.Registry)
	return registry
}

type shellOwnerKey struct{}

func shellOwnerFrom(ctx context.Context) string {
	owner, _ := ctx.Value(shellOwnerKey{}).(string)
	return owner
}

func NewBashTool(root string) (*BashTool, error) {
	return NewBashToolCached(root, nil)
}

func NewBashToolCached(root string, cache *ToolchainCache) (*BashTool, error) {
	resolved, err := NewRoot(root)
	if err != nil {
		return nil, err
	}
	probe := cache.probeFor(string(resolved), realToolchainRunner, konst.ToolchainProbeTimeoutMillis*time.Millisecond)
	choice, err := shell.Resolve("")
	if err != nil {
		return nil, err
	}
	return &BashTool{root: resolved, choice: choice, probe: probe}, nil
}

type toolchainProbe struct {
	done   chan struct{}
	states []interpreterState
}

func startToolchainProbe(dir string, run toolchainRunner, timeout time.Duration) *toolchainProbe {
	probe := &toolchainProbe{done: make(chan struct{})}
	go func() {
		probe.states = probeToolchain(dir, run, timeout)
		close(probe.done)
	}()
	return probe
}

func (p *toolchainProbe) wait() []interpreterState {
	<-p.done
	return p.states
}

type ToolchainCache struct {
	mu     sync.Mutex
	probes map[string]*toolchainProbe
}

func NewToolchainCache() *ToolchainCache {
	return &ToolchainCache{probes: make(map[string]*toolchainProbe)}
}

func (c *ToolchainCache) probeFor(dir string, run toolchainRunner, timeout time.Duration) *toolchainProbe {
	if c == nil {
		return startToolchainProbe(dir, run, timeout)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if probe, ok := c.probes[dir]; ok {
		return probe
	}
	probe := startToolchainProbe(dir, run, timeout)
	c.probes[dir] = probe
	return probe
}

type shellEnv struct{ setting string }

func realShellEnv() shellEnv { return shellEnv{} }

type RunShell struct {
	choice shell.Choice
	cache  *ToolchainCache
	run    *toolchainRunner
}

func (r RunShell) Resolved() bool { return r.cache != nil }

func (r RunShell) runner() toolchainRunner {
	if r.run == nil {
		return realToolchainRunner
	}
	return *r.run
}

func resolveRunShell(env shellEnv, run toolchainRunner) (RunShell, error) {
	choice, err := shell.Resolve(env.setting)
	if err != nil {
		return RunShell{}, err
	}
	return RunShell{choice: choice, cache: NewToolchainCache(), run: &run}, nil
}

func ResolveRunShell(override string) (RunShell, error) {
	return resolveRunShell(shellEnv{setting: override}, realToolchainRunner)
}

func NewBashToolFromShell(root string, shell RunShell) (*BashTool, error) {
	resolved, err := NewRoot(root)
	if err != nil {
		return nil, err
	}
	probe := shell.cache.probeFor(string(resolved), shell.runner(), konst.ToolchainProbeTimeoutMillis*time.Millisecond)
	return &BashTool{root: resolved, choice: shell.choice, probe: probe}, nil
}

func shellLinesFromShell(dir string, shell RunShell) []string {
	lines := []string{"shell: " + shell.choice.Label}
	if shell.choice.Note != "" {
		lines = append(lines, "shell notes: "+shell.choice.Note)
	}
	probe := shell.cache.probeFor(dir, shell.runner(), konst.ToolchainProbeTimeoutMillis*time.Millisecond)
	if summary := formatToolchain(probe.wait()); summary != "" {
		lines = append(lines, "toolchain: "+summary)
	}
	return lines
}

type interpreterState struct {
	Name    string
	Present bool
	Version string
}

type toolchainRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

func realToolchainRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

const versionUnread = "present, version unread in time"

var interpreterProbeArgs = map[string][]string{"go": {"version"}}

func probeArgsFor(name string) []string {
	if args, ok := interpreterProbeArgs[name]; ok {
		return args
	}
	return []string{"--version"}
}

func projectInterpreters(dir string) []string {
	var want []string
	if isFile(filepath.Join(dir, "package.json")) {
		want = append(want, "node", packageManager(dir))
	}
	if isFile(filepath.Join(dir, "go.mod")) {
		want = append(want, "go")
	}
	if isFile(filepath.Join(dir, "pyproject.toml")) {
		want = append(want, "python")
	}
	return want
}

func packageManager(dir string) string {
	switch {
	case isFile(filepath.Join(dir, "pnpm-lock.yaml")):
		return "pnpm"
	case isFile(filepath.Join(dir, "yarn.lock")):
		return "yarn"
	default:
		return "npm"
	}
}

func probeToolchain(dir string, run toolchainRunner, timeout time.Duration) []interpreterState {
	names := projectInterpreters(dir)
	states := make([]interpreterState, 0, len(names))
	for _, name := range names {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		out, err := run(ctx, name, probeArgsFor(name)...)
		cancel()
		switch {
		case errors.Is(err, exec.ErrNotFound):
			states = append(states, interpreterState{Name: name})
			continue
		case err != nil:
			states = append(states, interpreterState{Name: name, Present: true, Version: versionUnread})
			continue
		}
		version, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
		version = strings.TrimPrefix(version, name+" version ")
		states = append(states, interpreterState{Name: name, Present: true, Version: version})
	}
	return states
}

func formatToolchain(states []interpreterState) string {
	parts := make([]string, 0, len(states))
	for _, state := range states {
		if state.Present {
			parts = append(parts, state.Name+" "+state.Version)
		} else {
			parts = append(parts, state.Name+" missing")
		}
	}
	return strings.Join(parts, ", ")
}

func presentSummary(states []interpreterState) string {
	var present []string
	var missing bool
	for _, state := range states {
		if state.Present {
			present = append(present, state.Name+" "+state.Version)
		} else {
			missing = true
		}
	}
	if !missing {
		return ""
	}
	if len(present) == 0 {
		return "none of this project's interpreters are on this shell's PATH"
	}
	return "this shell's PATH has: " + strings.Join(present, ", ")
}

func missingInterpreterNamed(command string, states []interpreterState) (name, summary string, found bool) {
	for _, state := range states {
		if state.Present {
			continue
		}
		if !regexp.MustCompile(`\b` + regexp.QuoteMeta(state.Name) + `\b`).MatchString(command) {
			continue
		}
		return state.Name, presentSummary(states), true
	}
	return "", "", false
}

const bashToolName = "bash"

const commandExited = "the command exited %d\n"

const exitCommandNotFound = 127
const exitFoundButNotExecutable = 126

func bashFailureText(code int) string {
	switch code {
	case exitCommandNotFound:
		return fmt.Sprintf("bash: the command exited %d: not found in this environment", code)
	case exitFoundButNotExecutable:
		return fmt.Sprintf("bash: the command exited %d: found but not executable", code)
	default:
		return ""
	}
}

func (t *BashTool) Name() string { return bashToolName }

func (t *BashTool) Definition() llm.Tool {
	description := fmt.Sprintf(
		"runs one command in %s and waits for it to exit. cwd is already the working directory named in the environment block: spell paths that way, no cd. a nonzero exit is reported with its code. "+
			"a command is killed after %d ms and its output is lost, so a long one has to be narrowed or given a larger timeout_ms, up to %d. "+
			"do not use it to walk the tree: find, ls -R and wc descend into every ignored directory and take minutes here, "+
			"while glob, search and project_report skip what .gitignore skips and answer in milliseconds. "+
			"background: true is for long work that keeps running and nothing else: a dev server, a watcher, a long-running script. "+
			"a test, a build, an install, a version check or any other command that ends runs here without background, never in it. "+
			"a background start waits up to %d ms: a command that ends by then comes back with its output and exit code like any other and is not kept, "+
			"and one still running is kept on the shells screen and the call returns its name, pid and output so far. "+
			"start the server itself as the whole command, with no & and no nohup, so the process kept is the one that serves. "+
			"check whether it is up with check_port, which dials the port on localhost and answers in milliseconds, no http request needed.",
		t.choice.Label, konst.BashDeadlineMillis, konst.BashMaxDeadlineMillis, konst.BackgroundYieldMillis)
	if t.choice.Note != "" {
		description += " " + t.choice.Note
	}
	if toolchain := formatToolchain(t.probe.wait()); toolchain != "" {
		description += " this project's toolchain, probed once: " + toolchain + "."
	}
	return llm.Tool{
		Name:        bashToolName,
		Description: description,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command":    map[string]any{"type": "string", "description": "required unless check_port is set"},
				"timeout_ms": map[string]any{"type": "integer", "description": fmt.Sprintf("how long the command may run before it is killed, %d by default and %d at most. a larger number runs at the cap and says so rather than being refused", konst.BashDeadlineMillis, konst.BashMaxDeadlineMillis)},
				"background": map[string]any{"type": "boolean", "description": fmt.Sprintf("only for long work that keeps running: a dev server, a watcher, a long-running script. never a test or a one-shot command. waits up to %d ms, returns the output if it ended by then, and otherwise keeps it running on the shells screen, where it outlives the turn", konst.BackgroundYieldMillis)},
				"check_port": map[string]any{"type": "integer", "description": "skip command and report whether this port answers on 127.0.0.1, without any http request"},
			},
			"required": []string{},
		},
	}
}

type bashArgs struct {
	Command    string `json:"command"`
	TimeoutMS  int    `json:"timeout_ms,omitempty"`
	Background bool   `json:"background,omitempty"`
	CheckPort  int    `json:"check_port,omitempty"`
}

func (t *BashTool) checkPort(port int) Result {
	started := time.Now()
	open := shell.PortOpen("127.0.0.1", port, konst.PortCheckTimeoutMillis*time.Millisecond)
	took := time.Since(started).Milliseconds()
	state, code := "not listening", 1
	if open {
		state, code = "listening", 0
	}
	return Result{
		Content:  fmt.Sprintf("port %d is %s on 127.0.0.1, checked in %d ms", port, state, took),
		Command:  fmt.Sprintf("check_port %d", port),
		ExitCode: &code,
		Outcome:  ResultSucceeded,
	}
}

func (t *BashTool) command(ctx context.Context, command string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, t.choice.Path, "-c", command)
	cmd.Dir = string(t.root)
	cmd.Env = append(os.Environ(), subAgentDepthVar+"="+strconv.Itoa(processDepth()+1))
	cmd.WaitDelay = konst.BashWaitDelayMillis * time.Millisecond
	return cmd
}

func exitedResult(command, output string, code int, note string) Result {
	outcome := ResultSucceeded
	if code != 0 {
		output = strings.TrimRight(output, "\n")
		if output != "" {
			output += "\n"
		}
		output += fmt.Sprintf(commandExited, code)
		outcome = ResultFailed
	}
	return Result{Content: note + capResult(output), Command: command, ExitCode: &code, FailureText: bashFailureText(code), Outcome: outcome}
}

func (t *BashTool) runBackground(ctx context.Context, args bashArgs) (Result, error) {
	registry := shellRegistryFrom(ctx)
	if registry == nil {
		return Result{}, errors.New("bash: background needs a shell registry and none is attached to this turn")
	}
	ran, output, err := registry.Yield(ctx, t.command(context.Background(), args.Command), args.Command, shellOwnerFrom(ctx), konst.BackgroundYieldMillis*time.Millisecond)
	if err != nil {
		return Result{}, fmt.Errorf("bash: %w", err)
	}
	if ran.State == shell.Running {
		return Result{
			Content: capResult(fmt.Sprintf("%s is still running as pid %d in %s, and keeps running after this call: it is listed on the shells screen. check it with check_port once it should be up. its output so far:\n%s",
				ran.Name, ran.PID, ran.Dir, output)),
			Command: "background " + ran.Name + ": " + args.Command,
			Outcome: ResultSucceeded,
		}, nil
	}
	return exitedResult(args.Command, output, *ran.ExitCode, fmt.Sprintf(
		"bash: this ended inside the %d ms a background start waits, so nothing was kept on the shells screen: a command that ends belongs in bash without background\n",
		konst.BackgroundYieldMillis)), nil
}

func bashDeadline(requested int) (deadline int, corrected string) {
	switch {
	case requested > konst.BashMaxDeadlineMillis:
		return konst.BashMaxDeadlineMillis, fmt.Sprintf(
			"bash: timeout_ms %d is over the cap, so the command ran with the %d ms cap rather than being refused\n",
			requested, konst.BashMaxDeadlineMillis)
	case requested < 0:
		return konst.BashDeadlineMillis, fmt.Sprintf(
			"bash: timeout_ms %d is not a length of time, so the command ran with the default %d ms\n",
			requested, konst.BashDeadlineMillis)
	default:
		return cmp.Or(requested, konst.BashDeadlineMillis), ""
	}
}

func (t *BashTool) Run(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args bashArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("bash: arguments are not the expected shape: %w", err)
	}
	if args.CheckPort != 0 {
		return t.checkPort(args.CheckPort), nil
	}
	if strings.TrimSpace(args.Command) == "" {
		return Result{}, errors.New("bash: command is required unless check_port is set")
	}
	if name, summary, missing := missingInterpreterNamed(args.Command, t.probe.wait()); missing {
		return Result{}, fmt.Errorf("bash: %s is not on this shell's PATH, so this command would just fail not found. %s", name, summary)
	}
	if args.Background {
		return t.runBackground(ctx, args)
	}

	deadline, corrected := bashDeadline(args.TimeoutMS)
	ctx, cancel := context.WithTimeout(ctx, time.Duration(deadline)*time.Millisecond)
	defer cancel()

	started := time.Now()
	cmd := t.command(ctx, args.Command)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	tracked, startErr := shell.StartTracked(cmd)
	if startErr != nil {
		return Result{}, fmt.Errorf("bash: %w", startErr)
	}
	defer tracked.Release()
	runErr := cmd.Wait()

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return Result{}, errors.New(corrected + "bash: " + search.Note(search.Stopped, fmt.Sprintf(
			"%q ran %d ms, past the %d ms deadline. do not run it again unchanged: narrow it, or pass timeout_ms up to %d when the command truly needs longer. "+
				"a question about which files exist or what they contain is answered by project_report, glob or search without a shell and without this cost",
			args.Command, time.Since(started).Milliseconds(), deadline, konst.BashMaxDeadlineMillis)))
	}
	if ctx.Err() != nil {
		return Result{
			Content: capResult(output.String()),
			Command: args.Command,
			Outcome: ResultAborted,
			FailureText: fmt.Sprintf("bash: %q was cancelled elsewhere in this turn while it was running, not because the command itself failed. "+
				"its output so far is kept above", args.Command),
		}, nil
	}
	if cmd.ProcessState == nil {
		return Result{}, fmt.Errorf("bash: %w", runErr)
	}
	return exitedResult(args.Command, output.String(), cmd.ProcessState.ExitCode(), corrected), nil
}
