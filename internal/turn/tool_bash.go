package turn

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"boji/internal/konst"
	"boji/internal/llm"
	"boji/internal/search"
)

type BashTool struct {
	root      Root
	shell     string
	shellName string
	shellDir  string
}

func NewBashTool(root string) (*BashTool, error) {
	resolved, err := NewRoot(root)
	if err != nil {
		return nil, err
	}
	for _, posix := range []string{"sh", "bash"} {
		shell, lookErr := exec.LookPath(posix)
		if lookErr != nil {
			continue
		}
		family, dir, ok := askTheShellWhereItIs(shell, resolved)
		if !ok {
			family, dir = runtime.GOOS, string(resolved)
		}
		name := strings.TrimSuffix(filepath.Base(shell), ".exe")
		return &BashTool{root: resolved, shell: shell, shellName: name + " on " + family, shellDir: dir}, nil
	}
	return nil, errors.New("bash: no sh or bash on PATH, and cmd.exe is not a substitute: it mangles every quoted argument and understands none of the posix syntax this tool advertises")
}

func askTheShellWhereItIs(shell string, root Root) (string, string, bool) {
	cmd := exec.Command(shell, "-c", "uname -o; pwd")
	cmd.Dir = string(root)
	out, err := cmd.Output()
	if err != nil {
		return "", "", false
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 2 {
		return "", "", false
	}
	family, dir := strings.TrimSpace(lines[0]), strings.TrimSpace(lines[1])
	if family == "" || dir == "" {
		return "", "", false
	}
	return family, dir, true
}

func (t *BashTool) Name() string { return "bash" }

func (t *BashTool) Definition() llm.Tool {
	return llm.Tool{
		Name: "bash",
		Description: fmt.Sprintf(
			"runs one command in %s. cwd is already %s: spell paths that way, no cd. a nonzero exit is reported with its code. "+
				"a command is killed after %d ms and its output is lost, so a long one has to be narrowed or given a larger timeout_ms, up to %d. "+
				"do not use it to walk the tree: find, ls -R and wc descend into every ignored directory and take minutes here, "+
				"while glob, grep, search and project_report skip what .gitignore skips and answer in milliseconds.",
			t.shellName, t.shellDir, konst.BashDeadlineMillis, konst.BashMaxDeadlineMillis),
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
	TimeoutMS int    `json:"timeout_ms"`
}

func (t *BashTool) Run(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args bashArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("bash: arguments are not the expected shape: %w", err)
	}
	if strings.TrimSpace(args.Command) == "" {
		return Result{}, errors.New("bash: command is required")
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
	cmd.WaitDelay = konst.BashWaitDelayMillis * time.Millisecond

	output, runErr := cmd.CombinedOutput()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return Result{}, errors.New("bash: " + search.Note(search.Stopped, fmt.Sprintf(
			"%q ran %d ms, past the %d ms deadline. do not run it again unchanged: narrow it, or pass timeout_ms up to %d when the command truly needs longer. "+
				"a question about which files exist or what they contain is answered by project_report, glob, grep or search without a shell and without this cost",
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
		content += fmt.Sprintf("the command exited %d\n", code)
	}
	return Result{Content: content, Command: args.Command, ExitCode: &code}, nil
}
