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
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/search"
)

type BashTool struct {
	root       Root
	shell      string
	shellLabel string
	note       string
	probe      *toolchainProbe
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
	return &BashTool{
		root:       resolved,
		shell:      choice.Path,
		shellLabel: choice.Label,
		note:       choice.Note,
		probe:      probe,
	}, nil
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
	if override := env.getenv("TOFU_SHELL"); override != "" {
		return resolveShellOverride(env, override)
	}
	if env.goos == "windows" {
		return resolveWindowsShell(env)
	}
	return resolvePosixShell(env)
}

func resolveShellOverride(env shellEnv, setting string) (shellChoice, error) {
	path := setting
	if setting == "wsl" {
		found, err := env.lookPath("bash")
		if err != nil {
			return shellChoice{}, fmt.Errorf("bash: TOFU_SHELL=wsl but no bash is on PATH: %w", err)
		}
		path = found
	}
	if err := env.stat(path); err != nil {
		return shellChoice{}, fmt.Errorf("bash: TOFU_SHELL is set to %q and it does not exist: %w", path, err)
	}
	family, probeErr := env.probe(path)
	if probeErr != nil {
		return shellChoice{}, fmt.Errorf("bash: TOFU_SHELL is set to %q and it would not run: %w", path, probeErr)
	}
	if isWSLFamily(family) {
		return shellChoice{Path: path, Label: "wsl bash on " + family, Posix: true, WSL: true,
			Note: "wsl, chosen on purpose through TOFU_SHELL: its filesystem is separate from Windows and its PATH will not see software installed only on Windows"}, nil
	}
	return shellChoice{Path: path, Label: filepath.Base(path) + " on " + family, Posix: true}, nil
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
			"while glob, search and project_report skip what .gitignore skips and answer in milliseconds.",
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
				"command":    map[string]any{"type": "string"},
				"timeout_ms": map[string]any{"type": "integer", "description": fmt.Sprintf("how long the command may run before it is killed, %d by default and %d at most", konst.BashDeadlineMillis, konst.BashMaxDeadlineMillis)},
			},
			"required": []string{"command"},
		},
	}
}

type bashArgs struct {
	Command   string `json:"command"`
	TimeoutMS int    `json:"timeout_ms,omitempty"`
}

func (t *BashTool) Run(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args bashArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("bash: arguments are not the expected shape: %w", err)
	}
	if strings.TrimSpace(args.Command) == "" {
		return Result{}, errors.New("bash: command is required")
	}
	if name, summary, missing := missingInterpreterNamed(args.Command, t.probe.wait()); missing {
		return Result{}, fmt.Errorf("bash: %s is not on this shell's PATH, so this command would just fail not found. %s", name, summary)
	}

	deadline := cmp.Or(args.TimeoutMS, konst.BashDeadlineMillis)
	if deadline < 1 || deadline > konst.BashMaxDeadlineMillis {
		return Result{}, fmt.Errorf("bash: timeout_ms is %d and it has to be between 1 and %d", args.TimeoutMS, konst.BashMaxDeadlineMillis)
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(deadline)*time.Millisecond)
	defer cancel()

	started := time.Now()
	cmd := exec.CommandContext(ctx, t.shell, "-c", args.Command)
	cmd.Dir = string(t.root)
	cmd.Env = append(os.Environ(), subAgentDepthVar+"="+strconv.Itoa(processDepth()+1))
	cmd.WaitDelay = konst.BashWaitDelayMillis * time.Millisecond

	output, runErr := cmd.CombinedOutput()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return Result{}, errors.New("bash: " + search.Note(search.Stopped, fmt.Sprintf(
			"%q ran %d ms, past the %d ms deadline. do not run it again unchanged: narrow it, or pass timeout_ms up to %d when the command truly needs longer. "+
				"a question about which files exist or what they contain is answered by project_report, glob or search without a shell and without this cost",
			args.Command, time.Since(started).Milliseconds(), deadline, konst.BashMaxDeadlineMillis)))
	}
	if cmd.ProcessState == nil {
		return Result{}, fmt.Errorf("bash: %w", runErr)
	}
	code := cmd.ProcessState.ExitCode()
	content := string(output)
	if code != 0 {
		content = strings.TrimRight(content, "\n")
		if content != "" {
			content += "\n"
		}
		content += fmt.Sprintf(commandExited, code)
	}
	return Result{Content: content, Command: args.Command, ExitCode: &code, FailureText: bashFailureText(code)}, nil
}
