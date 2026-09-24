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
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/search"
	"tofu/internal/shell"
)

const shellRegisterAfterMillis = 3000

type BashTool struct {
	root       Root
	shell      string
	shellLabel string
	note       string
	probe      *toolchainProbe
	calls      atomic.Int64
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

type promotable struct {
	mu  sync.Mutex
	buf bytes.Buffer
	dst *os.File
}

func (p *promotable) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.buf.Write(b)
	if p.dst != nil {
		_, _ = p.dst.Write(b)
	}
	return len(b), nil
}

func (p *promotable) promote(dst *os.File) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, err := dst.Write(p.buf.Bytes()); err == nil {
		p.dst = dst
	}
}

func (p *promotable) String() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.buf.String()
}

type bashWatch struct {
	handle atomic.Pointer[shell.Watch]
	stopc  chan struct{}
	wg     sync.WaitGroup
}

func watchBash(registry *shell.Registry, name, command string, pid int, output *promotable) *bashWatch {
	if registry == nil {
		return nil
	}
	w := &bashWatch{stopc: make(chan struct{})}
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		select {
		case <-time.After(shellRegisterAfterMillis * time.Millisecond):
		case <-w.stopc:
			return
		}
		watch, err := registry.Watch(name, command)
		if err != nil {
			return
		}
		_ = watch.SetPID(pid)
		output.promote(watch.Writer())
		w.handle.Store(watch)
	}()
	return w
}

func (w *bashWatch) stop() {
	if w == nil {
		return
	}
	close(w.stopc)
	w.wg.Wait()
}

func (w *bashWatch) exited(code int) {
	if w == nil {
		return
	}
	if handle := w.handle.Load(); handle != nil {
		_ = handle.Finish(code)
	}
}

func (w *bashWatch) killed() {
	if w == nil {
		return
	}
	if handle := w.handle.Load(); handle != nil {
		_ = handle.Killed()
	}
}

func NewBashTool(root string) (*BashTool, error) {
	return newBashToolWithDeps(root, realShellEnv(), realToolchainRunner, nil)
}

func NewBashToolCached(root string, cache *ToolchainCache) (*BashTool, error) {
	return newBashToolWithDeps(root, realShellEnv(), realToolchainRunner, cache)
}

func newBashToolWithDeps(root string, env shellEnv, run toolchainRunner, cache *ToolchainCache) (*BashTool, error) {
	resolved, err := NewRoot(root)
	if err != nil {
		return nil, err
	}
	probe := cache.probeFor(string(resolved), run, konst.ToolchainProbeTimeoutMillis*time.Millisecond)
	choice, err := resolveShell(env)
	if err != nil {
		return nil, err
	}
	return newBashTool(resolved, choice, probe), nil
}

func newBashTool(resolved Root, choice shellChoice, probe *toolchainProbe) *BashTool {
	return &BashTool{root: resolved, shell: choice.Path, shellLabel: choice.Label, note: choice.Note, probe: probe}
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

type shellChoice struct {
	Path  string
	Label string
	Posix bool
	WSL   bool
	Note  string
}

type shellEnv struct {
	goos     string
	getenv   func(string) string
	lookPath func(string) (string, error)
	stat     func(string) error
	probe    func(string) (string, error)
	setting  string
}

func realShellEnv() shellEnv {
	return shellEnv{
		goos:     runtime.GOOS,
		getenv:   os.Getenv,
		lookPath: exec.LookPath,
		stat:     func(path string) error { _, err := os.Stat(path); return err },
		probe:    probeShellFamily,
	}
}

func probeShellFamily(path string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), konst.ShellProbeTimeoutMillis*time.Millisecond)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "-c", "uname -o").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

const posixSyntaxDoesNotApply = "this project's tools assume a posix shell: heredocs, $VAR, forward slashes and /dev/null do not work here, " +
	"and && and || are a parse error in Windows PowerShell 5.1"

func resolveShell(env shellEnv) (shellChoice, error) {
	if env.setting != "" {
		return resolveShellOverride(env, env.setting, "the shell setting")
	}
	if override := env.getenv("TOFU_SHELL"); override != "" {
		return resolveShellOverride(env, override, "TOFU_SHELL")
	}
	if env.goos == "windows" {
		return resolveWindowsShell(env)
	}
	return resolvePosixShell(env)
}

func resolveShellOverride(env shellEnv, setting, source string) (shellChoice, error) {
	path := setting
	if setting == "wsl" {
		found, err := env.lookPath("bash")
		if err != nil {
			return shellChoice{}, fmt.Errorf("bash: %s=wsl but no bash is on PATH: %w", source, err)
		}
		path = found
	}
	if err := env.stat(path); err != nil {
		return shellChoice{}, fmt.Errorf("bash: %s is set to %q and it does not exist: %w", source, path, err)
	}
	family, probeErr := env.probe(path)
	if probeErr != nil {
		return shellChoice{}, fmt.Errorf("bash: %s is set to %q and it would not run: %w", source, path, probeErr)
	}
	if isWSLFamily(family) {
		return shellChoice{Path: path, Label: "wsl bash on " + family, Posix: true, WSL: true,
			Note: "wsl, chosen on purpose through " + source + ": its filesystem is separate from Windows and its PATH will not see software installed only on Windows"}, nil
	}
	return shellChoice{Path: path, Label: filepath.Base(path) + " on " + family, Posix: true}, nil
}

type RunShell struct {
	choice shellChoice
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
	choice, err := resolveShell(env)
	if err != nil {
		return RunShell{}, err
	}
	return RunShell{choice: choice, cache: NewToolchainCache(), run: &run}, nil
}

func ResolveRunShell(override string) (RunShell, error) {
	env := realShellEnv()
	env.setting = override
	return resolveRunShell(env, realToolchainRunner)
}

func NewBashToolFromShell(root string, shell RunShell) (*BashTool, error) {
	resolved, err := NewRoot(root)
	if err != nil {
		return nil, err
	}
	probe := shell.cache.probeFor(string(resolved), shell.runner(), konst.ToolchainProbeTimeoutMillis*time.Millisecond)
	return newBashTool(resolved, shell.choice, probe), nil
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

func resolveWindowsShell(env shellEnv) (shellChoice, error) {
	var rejected []string
	if bash, ok := gitBash(env); ok {
		if family, err := env.probe(bash); err == nil {
			return shellChoice{Path: bash, Label: "git bash on " + family, Posix: true}, nil
		}
		rejected = append(rejected, bash+" (found but would not run)")
	} else {
		rejected = append(rejected, "git bash (git is not on PATH and Git\\bin\\bash.exe is not beside it)")
	}
	if path, err := env.lookPath("bash"); err == nil {
		family, probeErr := env.probe(path)
		if probeErr == nil && !isWSLFamily(family) && !looksLikeWSLPath(path) {
			return shellChoice{Path: path, Label: "bash on " + family, Posix: true}, nil
		}
		rejected = append(rejected, path+" (this is wsl, a separate filesystem namespace with its own PATH; set TOFU_SHELL=wsl to use it on purpose)")
	}
	for _, name := range []string{"pwsh", "powershell"} {
		if path, err := env.lookPath(name); err == nil {
			return shellChoice{Path: path, Label: name + ", " + posixSyntaxDoesNotApply, Posix: false,
				Note: "no posix shell was found (tried: " + strings.Join(rejected, "; ") + "). " + posixSyntaxDoesNotApply}, nil
		}
	}
	return shellChoice{}, fmt.Errorf(
		"bash: no shell resolved. tried %s, and neither pwsh nor powershell is on PATH either. "+
			"fixes: install Git for Windows, set TOFU_SHELL to a shell's path, or set TOFU_SHELL=wsl",
		strings.Join(rejected, "; "))
}

func gitBash(env shellEnv) (string, bool) {
	gitPath, err := env.lookPath("git")
	if err != nil {
		if programFiles := env.getenv("ProgramFiles"); programFiles != "" {
			candidate := filepath.Join(programFiles, "Git", "bin", "bash.exe")
			if env.stat(candidate) == nil {
				return candidate, true
			}
		}
		return "", false
	}
	gitRoot := filepath.Dir(filepath.Dir(gitPath))
	for _, candidate := range []string{
		filepath.Join(gitRoot, "bin", "bash.exe"),
		filepath.Join(filepath.Dir(gitPath), "bash.exe"),
	} {
		if env.stat(candidate) == nil {
			return candidate, true
		}
	}
	return "", false
}

func isWSLFamily(family string) bool {
	return strings.Contains(family, "GNU/Linux")
}

func looksLikeWSLPath(path string) bool {
	lower := strings.ToLower(path)
	return strings.Contains(lower, `\system32\bash.exe`) || strings.Contains(lower, `windowsapps\bash.exe`)
}

func resolvePosixShell(env shellEnv) (shellChoice, error) {
	if sh := env.getenv("SHELL"); sh != "" && env.stat(sh) == nil {
		family, _ := env.probe(sh)
		return shellChoice{Path: sh, Label: filepath.Base(sh) + " on " + cmp.Or(family, env.goos), Posix: true}, nil
	}
	if env.stat("/bin/sh") == nil {
		family, _ := env.probe("/bin/sh")
		return shellChoice{Path: "/bin/sh", Label: "sh on " + cmp.Or(family, env.goos), Posix: true}, nil
	}
	return shellChoice{}, errors.New(
		"bash: $SHELL is not set and /bin/sh does not exist. fixes: set $SHELL to your shell, or install a posix shell at /bin/sh")
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
		if err != nil {
			states = append(states, interpreterState{Name: name})
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
		"runs one command in %s. cwd is already the working directory named in the environment block: spell paths that way, no cd. a nonzero exit is reported with its code. "+
			"a command is killed after %d ms and its output is lost, so a long one has to be narrowed or given a larger timeout_ms, up to %d. "+
			"do not use it to walk the tree: find, ls -R and wc descend into every ignored directory and take minutes here, "+
			"while glob, search and project_report skip what .gitignore skips and answer in milliseconds. "+
			"a server or any other command that does not exit belongs in background: true, which starts it and returns immediately rather than waiting for it to exit; "+
			"check whether it is up with check_port, which dials the port on localhost and answers in milliseconds, no http request needed.",
		t.shellLabel, konst.BashDeadlineMillis, konst.BashMaxDeadlineMillis)
	if t.note != "" {
		description += " " + t.note
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
				"background": map[string]any{"type": "boolean", "description": "start command and return right away instead of waiting for it to exit; it outlives the turn and keeps running until it exits on its own or tofu exits"},
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

func (t *BashTool) runBackground(ctx context.Context, args bashArgs) (Result, error) {
	registry := shellRegistryFrom(ctx)
	if registry == nil {
		return Result{}, errors.New("bash: background needs a shell registry and none is attached to this turn")
	}
	name := t.nextShellName()
	entry, err := registry.Start(string(t.root), name, args.Command)
	if err != nil {
		return Result{}, fmt.Errorf("bash: %w", err)
	}
	return Result{
		Content: fmt.Sprintf("started %s as pid %d, not waited on: it keeps running after this call returns. check it with check_port once it should be up", name, entry.PID),
		Command: fmt.Sprintf("background %s: %s", name, args.Command),
		Outcome: ResultSucceeded,
	}, nil
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
	if args.Background {
		return t.runBackground(ctx, args)
	}
	if name, summary, missing := missingInterpreterNamed(args.Command, t.probe.wait()); missing {
		return Result{}, fmt.Errorf("bash: %s is not on this shell's PATH, so this command would just fail not found. %s", name, summary)
	}

	deadline, corrected := bashDeadline(args.TimeoutMS)
	ctx, cancel := context.WithTimeout(ctx, time.Duration(deadline)*time.Millisecond)
	defer cancel()

	started := time.Now()
	cmd := exec.CommandContext(ctx, t.shell, "-c", args.Command)
	cmd.Dir = string(t.root)
	cmd.Env = append(os.Environ(), subAgentDepthVar+"="+strconv.Itoa(processDepth()+1))
	cmd.WaitDelay = konst.BashWaitDelayMillis * time.Millisecond

	output := &promotable{}
	cmd.Stdout, cmd.Stderr = output, output
	tracked, startErr := shell.StartTracked(cmd)
	if startErr != nil {
		return Result{}, fmt.Errorf("bash: %w", startErr)
	}
	defer tracked.Release()

	watch := watchBash(shellRegistryFrom(ctx), t.nextShellName(), args.Command, cmd.Process.Pid, output)
	runErr := cmd.Wait()
	watch.stop()

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		watch.killed()
		return Result{}, errors.New(corrected + "bash: " + search.Note(search.Stopped, fmt.Sprintf(
			"%q ran %d ms, past the %d ms deadline. do not run it again unchanged: narrow it, or pass timeout_ms up to %d when the command truly needs longer. "+
				"a question about which files exist or what they contain is answered by project_report, glob or search without a shell and without this cost",
			args.Command, time.Since(started).Milliseconds(), deadline, konst.BashMaxDeadlineMillis)))
	}
	if ctx.Err() != nil {
		watch.killed()
		return Result{
			Content: capResult(output.String()),
			Command: args.Command,
			Outcome: ResultAborted,
			FailureText: fmt.Sprintf("bash: %q was cancelled elsewhere in this turn while it was running, not because the command itself failed. "+
				"its output so far is kept above", args.Command),
		}, nil
	}
	if cmd.ProcessState == nil {
		watch.killed()
		return Result{}, fmt.Errorf("bash: %w", runErr)
	}
	code := cmd.ProcessState.ExitCode()
	watch.exited(code)
	content := output.String()
	outcome := ResultSucceeded
	if code != 0 {
		content = strings.TrimRight(content, "\n")
		if content != "" {
			content += "\n"
		}
		content += fmt.Sprintf(commandExited, code)
		outcome = ResultFailed
	}
	content = corrected + capResult(content)
	return Result{Content: content, Command: args.Command, ExitCode: &code, FailureText: bashFailureText(code), Outcome: outcome}, nil
}

func (t *BashTool) nextShellName() string {
	return "bash-" + strconv.FormatInt(t.calls.Add(1), 10)
}
