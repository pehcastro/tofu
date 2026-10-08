package turn

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/session"
	"tofu/internal/shell"
)

type BashTool struct {
	root      Root
	choice    shell.Choice
	probe     *toolchainProbe
	softLimit time.Duration
}

type shellRegistryKey struct{}

func WithShellRegistry(ctx context.Context, registry *shell.Registry) context.Context {
	if registry == nil {
		return ctx
	}
	return context.WithValue(ctx, shellRegistryKey{}, registry)
}

func ShellRegistryFrom(ctx context.Context) *shell.Registry {
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
	return &BashTool{root: resolved, choice: choice, probe: probe, softLimit: konst.BashSoftLimitMillis * time.Millisecond}, nil
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
	return &BashTool{root: resolved, choice: shell.choice, probe: probe, softLimit: konst.BashSoftLimitMillis * time.Millisecond}, nil
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
	if body, err := os.ReadFile(filepath.Join(dir, "package.json")); err == nil {
		var manifest packageManifest
		_ = json.Unmarshal(body, &manifest)
		want = append(want, "node", lockfileManager(dir, manifest))
	}
	if isFile(filepath.Join(dir, "go.mod")) {
		want = append(want, "go")
	}
	if isFile(filepath.Join(dir, "pyproject.toml")) {
		want = append(want, "python")
	}
	return want
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

const (
	bashToolName  = "bash"
	ShellToolName = "shell"
)

const (
	commandExited         = "the command exited %d\n"
	cancelledWhileRunning = "bash: %q was cancelled elsewhere in this turn while it was running, not because the command itself failed. its output so far is kept above"
)

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
			"a command still running after %d ms, such as a long build or test run, moves to a background shell and keeps running: the call returns its name, such as bash-1, and the output so far, and the shell tool's wait reads it back until it ends. "+
			"never wait on a running command with another bash call, such as sleep, Wait-Process or a polling loop: use shell wait. a timeout_ms at or under %d is a hard limit instead: the command is killed there and returns what it printed. "+
			"do not use it to walk the tree: find, ls -R and wc descend into every ignored directory and take minutes here, "+
			"while glob, search and project_report skip what .gitignore skips and answer in milliseconds. "+
			"background: true is for long work that keeps running and nothing else: a dev server, a watcher, a long-running script. "+
			"a test, a build, an install, a version check or any other command that ends runs here without background, never in it. "+
			"a background start waits up to %d ms: a command that ends by then comes back with its output and exit code like any other and is not kept, "+
			"and one still running is kept on the shells screen and the call returns its name, pid and output so far, as soon as its port opens or it prints a ready line. "+
			"a start whose command, env or package script names a port that another process holds on 127.0.0.1 or ::1 is refused, naming the holder and a free port. "+
			"start the server itself as the whole command, with no & and no nohup, so the process kept is the one that serves. "+
			"stop, restart and read a kept process with the shell tool by its name, never with kill, taskkill or pkill: a kill of a pid tofu started runs as shell stop. "+
			"check whether a port is taken with check_port, which dials it on 127.0.0.1 and ::1 and names the process holding it, no http request needed.",
		t.choice.Label, t.softLimit.Milliseconds(), t.softLimit.Milliseconds(), konst.BackgroundYieldMillis)
	if t.choice.Note != "" {
		description += " " + t.choice.Note
	}
	return llm.Tool{
		Name:        bashToolName,
		Description: description,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command":    map[string]any{"type": "string", "description": "required unless check_port is set"},
				"timeout_ms": map[string]any{"type": "integer", "description": fmt.Sprintf("only for a command that must be killed early: at or under %d it is killed at this limit. a larger value, or none, lets a command still running at %d ms move to a background shell", t.softLimit.Milliseconds(), t.softLimit.Milliseconds())},
				"background": map[string]any{"type": "boolean", "description": fmt.Sprintf("only for long work that keeps running: a dev server, a watcher, a long-running script. never a test or a one-shot command. waits up to %d ms, returns the output if it ended by then, and otherwise keeps it running on the shells screen, where it outlives the turn", konst.BackgroundYieldMillis)},
				"check_port": map[string]any{"type": "integer", "description": "skip command and report whether this port is free or held on 127.0.0.1 and ::1, with the pid and command line of its holder, without any http request"},
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

func (t *BashTool) checkPort(ctx context.Context, port int) Result {
	started := time.Now()
	lookup, cancel := context.WithTimeout(ctx, konst.PortHolderTimeoutMillis*time.Millisecond)
	defer cancel()
	var lines []string
	for _, address := range shell.Probe(lookup, port, konst.PortCheckTimeoutMillis*time.Millisecond) {
		state := "free"
		if address.Open {
			state = "held by " + address.Holder()
		}
		lines = append(lines, address.Host+": "+state)
	}
	code := 0
	return Result{
		Content:  fmt.Sprintf("port %d, checked in %d ms\n%s", port, time.Since(started).Milliseconds(), strings.Join(lines, "\n")),
		Command:  fmt.Sprintf("check_port %d", port),
		ExitCode: &code,
		Outcome:  ResultSucceeded,
	}
}

func (t *BashTool) command(ctx context.Context, command string) *exec.Cmd {
	cmd := t.choice.Command(ctx, string(t.root), command, subAgentDepthVar+"="+strconv.Itoa(processDepth()+1))
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
	return Result{Content: note + output, Command: command, ExitCode: &code, FailureText: bashFailureText(code), Outcome: outcome}
}

func (t *BashTool) runBackground(ctx context.Context, args bashArgs) (Result, error) {
	registry := ShellRegistryFrom(ctx)
	if registry == nil {
		return Result{}, errors.New("bash: background needs a shell registry and none is attached to this turn")
	}
	cmd := t.command(context.Background(), args.Command)
	got, err := registry.YieldReady(ctx, cmd, args.Command, shellOwnerFrom(ctx), shell.Wait{
		Within: konst.BackgroundYieldMillis * time.Millisecond,
		Poll:   konst.ReadyPollMillis * time.Millisecond,
		Port:   shell.NamedPort(string(t.root), args.Command, cmd.Env),
		Kept:   shell.KeptBackground,
	})
	if err != nil {
		return Result{}, fmt.Errorf("bash: %w", err)
	}
	ran := got.Shell
	if ran.State == shell.Running {
		return Result{
			Content: fmt.Sprintf("%s is still running as pid %d in %s, and keeps running after this call: it is listed on the shells screen. this call returned after %d ms because %s. its output so far:\n%s",
				ran.Name, ran.PID, ran.Dir, got.Took.Milliseconds(), got.Ready.Words(), got.Output),
			Command: "background " + ran.Name + ": " + args.Command,
			Outcome: ResultSucceeded,
		}, nil
	}
	return exitedResult(args.Command, got.Output, *ran.ExitCode, fmt.Sprintf(
		"bash: this ended inside the %d ms a background start waits, so nothing was kept on the shells screen and there is no shell name to wait on: a command that ends belongs in bash without background\n",
		konst.BackgroundYieldMillis)), nil
}

func deadlineResult(command, output string, deadline time.Duration) Result {
	killed := fmt.Sprintf("bash: %s: %q was killed at its deadline and its output until then is above. do not run it again unchanged: narrow it, or pass timeout_ms up to %d when the command truly needs longer. "+
		"a question about which files exist or what they contain is answered by project_report, glob or search without a shell and without this cost",
		shell.HitDeadline(deadline), command, konst.BashMaxDeadlineMillis)
	return Result{Content: output + "\n" + killed, Command: command, Outcome: ResultFailed, FailureText: killed}
}

func (t *BashTool) runOrMove(ctx context.Context, registry *shell.Registry, command, corrected string, deadline time.Duration) (Result, error) {
	wait := shell.Wait{Within: min(deadline, t.softLimit)}
	if deadline > t.softLimit {
		wait.Kept = shell.KeptMoved
	}
	got, err := registry.YieldReady(ctx, t.command(context.Background(), command), command, shellOwnerFrom(ctx), wait)
	if err != nil {
		return Result{}, fmt.Errorf("bash: %w", err)
	}
	ran := got.Shell
	if got.Ready == shell.ReadyWaited && deadline <= t.softLimit {
		if err := registry.KillAtDeadline(ran.Name, deadline); err != nil && !errors.Is(err, shell.ErrNotRunning) {
			return Result{}, fmt.Errorf("bash: %w", err)
		}
		return deadlineResult(command, corrected+got.Output, deadline), nil
	}
	switch got.Ready {
	case shell.ReadyExited:
		output := &heldOutput{}
		_, _ = output.Write([]byte(got.Output))
		text, dropped := output.text()
		return exitedResult(command, text, *ran.ExitCode, dropped+corrected), nil
	case shell.ReadyStopped:
		return Result{
			Content:     got.Output,
			Command:     command,
			Outcome:     ResultAborted,
			FailureText: fmt.Sprintf(cancelledWhileRunning, command),
		}, registry.Kill(ran.Name)
	}
	return Result{
		Content: fmt.Sprintf("%s is still running after %d ms, so it moved to a background shell as pid %d instead of holding this call. it was not killed and keeps running, listed on the shells screen. "+
			"read it back with the shell tool: wait %s returns as soon as it ends, or after %d ms, with its exit code and last lines; logs %s returns its last lines now. "+
			"never wait on it with another bash call. its output so far:\n%s",
			ran.Name, got.Took.Milliseconds(), ran.PID, ran.Name, t.softLimit.Milliseconds(), ran.Name, got.Output),
		Command: "background " + ran.Name + ": " + command,
		Outcome: ResultSucceeded,
	}, nil
}

func OwnShellsCalled(ctx context.Context, request GateRequest) []string {
	registry := ShellRegistryFrom(ctx)
	var args struct {
		Command string `json:"command"`
		Name    string `json:"name"`
	}
	if registry == nil || json.Unmarshal(request.Args, &args) != nil {
		return nil
	}
	switch request.Tool {
	case ShellToolName:
		if _, err := registry.Read(args.Name); err == nil {
			return []string{args.Name}
		}
	case bashToolName:
		var names []string
		for _, one := range registry.Owning(args.Command) {
			names = append(names, one.Name)
		}
		return names
	}
	return nil
}

func stopOwned(registry *shell.Registry, command string, owned []shell.Shell) (Result, error) {
	names := make([]string, len(owned))
	for i, one := range owned {
		if err := registry.Kill(one.Name); err != nil {
			return Result{}, fmt.Errorf("bash: %q kills %s, which tofu started, so it ran as shell stop, and the stop failed: %w", command, one.Name, err)
		}
		names[i] = one.Name
	}
	stopped := strings.Join(names, ", ")
	return Result{
		Content: fmt.Sprintf("bash: %q kills %s, which tofu started, so it ran as shell stop %s instead: every process in its tree is gone, children included. next time use the shell tool to stop or restart it", command, stopped, stopped),
		Command: ShellToolName + " stop " + stopped,
		Outcome: ResultSucceeded,
	}, nil
}

const (
	commandPosition = `(?:^|[;&|(` + "`" + `])\s*`
	sleepPattern    = commandPosition + `sleep\s+(\d+(?:\.\d+)?)([smh]?)\b`
	killPattern     = commandPosition + `(?:\S*[/\\])?(?:kill|pkill|taskkill)(?:\.exe)?(?:\s|$)`
)

func subAgentRefusal(args bashArgs) error {
	command := args.Command
	var waits [][]string
	if !args.Background {
		waits = regexp.MustCompile(sleepPattern).FindAllStringSubmatch(command, -1)
	}
	for _, match := range waits {
		slept, err := time.ParseDuration(match[1] + cmp.Or(match[2], "s"))
		if err == nil && slept > konst.SubAgentSleepSeconds*time.Second {
			return fmt.Errorf("bash: sub_agent_boundaries: %q sleeps %s, and a sub-agent never waits on another's files, so no sleep over %d s runs here. "+
				"work on the paths you own and report what you still need from a sibling instead of waiting for it", command, slept, konst.SubAgentSleepSeconds)
		}
	}
	if regexp.MustCompile(killPattern).MatchString(command) {
		return fmt.Errorf("bash: %q is refused: a sub-agent kills nothing but a shell tofu started for it, which runs as shell stop. "+
			"check the app with the framework's in-process request, such as app.request or a test client, and leave servers to the orchestrator", command)
	}
	return nil
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
		return t.checkPort(ctx, args.CheckPort), nil
	}
	if strings.TrimSpace(args.Command) == "" {
		return Result{}, errors.New("bash: command is required unless check_port is set")
	}
	owner := shellOwnerFrom(ctx)
	subAgent := owner != "" && owner != session.AuthorOrchestrator
	if registry := ShellRegistryFrom(ctx); registry != nil {
		owned := registry.Owning(args.Command)
		startedElsewhere := slices.ContainsFunc(owned, func(one shell.Shell) bool { return one.Owner != owner })
		if len(owned) > 0 && (!subAgent || !startedElsewhere) {
			return stopOwned(registry, args.Command, owned)
		}
	}
	if subAgent {
		if err := subAgentRefusal(args); err != nil {
			return Result{}, err
		}
	}
	if name, summary, missing := missingInterpreterNamed(args.Command, t.probe.wait()); missing {
		return Result{}, fmt.Errorf("bash: %s is not on this shell's PATH, so this command would just fail not found. %s", name, summary)
	}
	if args.Background {
		return t.runBackground(ctx, args)
	}

	millis, corrected := bashDeadline(args.TimeoutMS)
	deadline := time.Duration(millis) * time.Millisecond
	if registry := ShellRegistryFrom(ctx); registry != nil {
		return t.runOrMove(ctx, registry, args.Command, corrected, deadline)
	}
	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()

	cmd := t.command(ctx, args.Command)
	output := &heldOutput{}
	cmd.Stdout, cmd.Stderr = output, output
	tracked, startErr := shell.StartTracked(cmd)
	if startErr != nil {
		return Result{}, fmt.Errorf("bash: %w", startErr)
	}
	defer tracked.Release()
	runErr := cmd.Wait()
	text, dropped := output.text()
	note := dropped + corrected

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return deadlineResult(args.Command, note+text, deadline), nil
	}
	if ctx.Err() != nil {
		return Result{
			Content:     note + text,
			Command:     args.Command,
			Outcome:     ResultAborted,
			FailureText: fmt.Sprintf(cancelledWhileRunning, args.Command),
		}, nil
	}
	if cmd.ProcessState == nil {
		return Result{}, fmt.Errorf("bash: %w", runErr)
	}
	return exitedResult(args.Command, text, cmd.ProcessState.ExitCode(), note), nil
}

type heldOutput struct {
	head, tail []byte
	total      int
}

func (h *heldOutput) Write(p []byte) (int, error) {
	h.total += len(p)
	half := konst.BashOutputHeldBytes / 2
	taken := min(half-len(h.head), len(p))
	h.head = append(h.head, p[:taken]...)
	h.tail = append(h.tail, p[taken:]...)
	if len(h.tail) > 2*half {
		h.tail = append(h.tail[:0], h.tail[len(h.tail)-half:]...)
	}
	return len(p), nil
}

func (h *heldOutput) text() (string, string) {
	head, tail := h.head, h.tail[max(0, len(h.tail)-konst.BashOutputHeldBytes/2):]
	if len(head)+len(tail) == h.total {
		return shell.Decode(slices.Concat(head, tail)), ""
	}
	for cut := len(head) - 1; cut >= max(0, len(head)-utf8.UTFMax); cut-- {
		if utf8.RuneStart(head[cut]) {
			if !utf8.FullRune(head[cut:]) {
				head = head[:cut]
			}
			break
		}
	}
	for skipped := 0; skipped < utf8.UTFMax-1 && len(tail) > 0 && !utf8.RuneStart(tail[0]); skipped++ {
		tail = tail[1:]
	}
	dropped := h.total - len(head) - len(tail)
	return shell.Decode(slices.Concat(head, fmt.Appendf(nil, "\n...(%d bytes dropped here as they arrived)...\n", dropped), tail)), fmt.Sprintf(
		heldDropLead+"%d bytes and a call holds %d while it runs, so the %d in the middle were dropped as they arrived\n",
		h.total, konst.BashOutputHeldBytes, dropped)
}
