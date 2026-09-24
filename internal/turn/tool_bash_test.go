package turn

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/subagent"
)

func fakeShellEnv() shellEnv {
	return shellEnv{
		goos:     "windows",
		getenv:   func(string) string { return "" },
		lookPath: func(string) (string, error) { return "", os.ErrNotExist },
		stat:     func(string) error { return os.ErrNotExist },
		probe:    func(string) (string, error) { return "", os.ErrNotExist },
	}
}

func TestGitBashIsFoundThroughGitRatherThanPATHWhenTheOnlyBashOnPathIsTheWSLLauncher(t *testing.T) {
	env := fakeShellEnv()
	env.lookPath = func(name string) (string, error) {
		switch name {
		case "git":
			return `C:\Program Files\Git\cmd\git.exe`, nil
		case "bash":
			return `C:\Windows\system32\bash.exe`, nil
		}
		return "", os.ErrNotExist
	}
	env.stat = func(path string) error {
		if path == `C:\Program Files\Git\bin\bash.exe` {
			return nil
		}
		return os.ErrNotExist
	}
	env.probe = func(path string) (string, error) {
		if path == `C:\Program Files\Git\bin\bash.exe` {
			return "Msys", nil
		}
		return "GNU/Linux", nil
	}

	choice, err := resolveShell(env)
	if err != nil {
		t.Fatalf("resolving the shell: %v", err)
	}
	if choice.Path != `C:\Program Files\Git\bin\bash.exe` {
		t.Fatalf("resolved %q, a machine whose only bash on PATH is the WSL launcher should still find git bash", choice.Path)
	}
	if !strings.Contains(choice.Label, "git bash") || !strings.Contains(choice.Label, "Msys") {
		t.Fatalf("the label does not say git bash on Msys: %q", choice.Label)
	}
}

func TestAWSLShapedShellIsNeverChosenImplicitly(t *testing.T) {
	env := fakeShellEnv()
	env.lookPath = func(name string) (string, error) {
		switch name {
		case "bash":
			return `C:\Windows\system32\bash.exe`, nil
		case "pwsh":
			return `C:\Program Files\PowerShell\7\pwsh.exe`, nil
		}
		return "", os.ErrNotExist
	}
	env.probe = func(string) (string, error) { return "GNU/Linux", nil }

	choice, err := resolveShell(env)
	if err != nil {
		t.Fatalf("resolving the shell: %v", err)
	}
	if choice.WSL {
		t.Fatalf("a WSL-shaped shell was chosen with no setting asking for it: %+v", choice)
	}
	if choice.Posix {
		t.Fatalf("the fallback is not posix, it landed on %+v", choice)
	}
	if !strings.Contains(choice.Note, "TOFU_SHELL=wsl") {
		t.Fatalf("the refusal to auto-pick wsl never says how to opt in: %q", choice.Note)
	}
}

func TestWhenNothingResolvesOnWindowsTheRefusalNamesTheOptions(t *testing.T) {
	_, err := resolveShell(fakeShellEnv())
	if err == nil {
		t.Fatal("every candidate was absent and a shell still resolved")
	}
	for _, want := range []string{"git bash", "pwsh", "powershell", "TOFU_SHELL"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal never names %q: %v", want, err)
		}
	}
}

func TestWhenNothingResolvesOnAPosixMachineTheRefusalNamesTheOptions(t *testing.T) {
	env := fakeShellEnv()
	env.goos = "linux"
	_, err := resolveShell(env)
	if err == nil {
		t.Fatal("SHELL was unset and /bin/sh was absent, and a shell still resolved")
	}
	for _, want := range []string{"SHELL", "/bin/sh"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal never names %q: %v", want, err)
		}
	}
}

func TestOnAPosixMachineSHELLDecidesTheShell(t *testing.T) {
	env := fakeShellEnv()
	env.goos = "linux"
	env.getenv = func(name string) string {
		if name == "SHELL" {
			return "/usr/bin/zsh"
		}
		return ""
	}
	env.stat = func(path string) error {
		if path == "/usr/bin/zsh" {
			return nil
		}
		return os.ErrNotExist
	}
	env.probe = func(string) (string, error) { return "Linux", nil }

	choice, err := resolveShell(env)
	if err != nil {
		t.Fatalf("resolving the shell: %v", err)
	}
	if choice.Path != "/usr/bin/zsh" {
		t.Fatalf("$SHELL said /usr/bin/zsh and tofu resolved %q instead", choice.Path)
	}
	if !strings.Contains(choice.Label, "zsh") {
		t.Fatalf("the label does not name the person's own shell: %q", choice.Label)
	}
}

func TestASettingOverridesTheResolution(t *testing.T) {
	env := fakeShellEnv()
	env.lookPath = func(name string) (string, error) {
		if name == "git" {
			return `C:\Program Files\Git\cmd\git.exe`, nil
		}
		return "", os.ErrNotExist
	}
	env.stat = func(path string) error {
		if path == `C:\Program Files\Git\bin\bash.exe` || path == `D:\custom\shell.exe` {
			return nil
		}
		return os.ErrNotExist
	}
	env.probe = func(string) (string, error) { return "Msys", nil }
	env.getenv = func(name string) string {
		if name == "TOFU_SHELL" {
			return `D:\custom\shell.exe`
		}
		return ""
	}

	choice, err := resolveShell(env)
	if err != nil {
		t.Fatalf("resolving the shell: %v", err)
	}
	if choice.Path != `D:\custom\shell.exe` {
		t.Fatalf("TOFU_SHELL was set and git bash still won: %+v", choice)
	}
}

func TestTheSettingOffersWSLOnPurpose(t *testing.T) {
	env := fakeShellEnv()
	env.lookPath = func(name string) (string, error) {
		if name == "bash" {
			return `C:\Windows\system32\bash.exe`, nil
		}
		return "", os.ErrNotExist
	}
	env.stat = func(string) error { return nil }
	env.probe = func(string) (string, error) { return "GNU/Linux", nil }
	env.getenv = func(name string) string {
		if name == "TOFU_SHELL" {
			return "wsl"
		}
		return ""
	}

	choice, err := resolveShell(env)
	if err != nil {
		t.Fatalf("resolving the shell: %v", err)
	}
	if !choice.WSL {
		t.Fatalf("TOFU_SHELL=wsl did not choose wsl: %+v", choice)
	}
	if !strings.Contains(choice.Note, "wsl") {
		t.Fatalf("the block never says wsl is in use: %q", choice.Note)
	}
}

func TestTheToolchainProbeRunsOnceAcrossAThreeStepTurn(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "go.mod"), "module x\n")
	var calls int
	run := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		calls++
		return []byte("go1.99.0\n"), nil
	}
	env := fakeShellEnv()
	env.goos = "linux"
	env.stat = func(string) error { return nil }
	env.probe = func(string) (string, error) { return "Linux", nil }

	tool, err := newBashToolWithDeps(root, env, run)
	if err != nil {
		t.Fatalf("building the bash tool: %v", err)
	}
	if calls != 1 {
		t.Fatalf("building the tool probed the toolchain %d times, wanted 1", calls)
	}
	for range 3 {
		tool.Definition()
	}
	if calls != 1 {
		t.Fatalf("the probe ran %d times across a three step turn, wanted 1", calls)
	}
}

func TestAHangingProbeDoesNotStallTheTurn(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "go.mod"), "module x\n")
	hang := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}

	started := time.Now()
	states := probeToolchain(dir, hang, konst.ToolchainProbeTimeoutMillis*time.Millisecond)
	took := time.Since(started)

	if took > 3*time.Second {
		t.Fatalf("a hanging probe held the turn for %v", took)
	}
	if len(states) != 1 || states[0].Present {
		t.Fatalf("a probe that never returns should be recorded as absent: %+v", states)
	}
}

func TestABashCallNamingAMissingInterpreterNamesItAndWhatTheShellHas(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "go.mod"), "module x\n")
	absent := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return nil, exec.ErrNotFound
	}
	env := fakeShellEnv()
	env.goos = "linux"
	env.stat = func(string) error { return nil }
	env.probe = func(string) (string, error) { return "Linux", nil }

	tool, err := newBashToolWithDeps(root, env, absent)
	if err != nil {
		t.Fatalf("building the bash tool: %v", err)
	}
	args, err := json.Marshal(bashArgs{Command: "go build ./..."})
	if err != nil {
		t.Fatal(err)
	}
	_, runErr := tool.Run(context.Background(), args)
	if runErr == nil {
		t.Fatal("a command naming a missing interpreter ran anyway")
	}
	for _, want := range []string{"go", "not on this shell's PATH", "none of this project's interpreters are on this shell's PATH"} {
		if !strings.Contains(runErr.Error(), want) {
			t.Fatalf("the refusal never says %q: %v", want, runErr)
		}
	}
	t.Logf("missing interpreter refusal: %v", runErr)
}

func bashCall(id, command string) llm.Decision {
	args, err := json.Marshal(bashArgs{Command: command})
	if err != nil {
		panic(err)
	}
	return toolCallDecision(llm.ToolCall{ID: id, Name: "bash", Arguments: args})
}

func TestAStoppedCommandIsAFailedResultRatherThanTheEndOfTheTurn(t *testing.T) {
	root := t.TempDir()
	bash, err := NewBashTool(root)
	if err != nil {
		t.Fatalf("building the bash tool: %v", err)
	}
	stopped, err := json.Marshal(bashArgs{Command: "sleep 30", TimeoutMS: 300})
	if err != nil {
		t.Fatal(err)
	}
	model := &stubModel{decisions: []llm.Decision{
		toolCallDecision(llm.ToolCall{ID: "call-1", Name: "bash", Arguments: stopped}),
		bashCall("call-2", "echo the model went on > note.txt"),
		messageDecision(),
	}}

	row, err := Run(context.Background(), baseConfig(t, model, NewRegistry(bash)))
	if err != nil {
		t.Fatalf("a stopped command ended the turn: %v", err)
	}

	first := firstToolCall(t, row)
	if !strings.Contains(first.Error, "degraded stopped") {
		t.Fatalf("the stopped command did not reach the model as a failed result: %+v", first)
	}
	went, readErr := os.ReadFile(filepath.Join(root, "note.txt"))
	if readErr != nil || !strings.Contains(string(went), "the model went on") {
		t.Fatalf("the model never got to do anything after the stop: %q %v", went, readErr)
	}
	last := row.Steps[len(row.Steps)-1]
	if last.AssistantText != "done" {
		t.Fatalf("the turn did not finish with the model's own answer: %+v", last)
	}
	showRow(t, "a stopped command inside a finished turn", row)
}

func parentTurnWithAShell(t *testing.T, root string, decisions []llm.Decision) (Config, *SpawnTool) {
	t.Helper()
	bash, err := NewBashTool(root)
	if err != nil {
		t.Fatalf("building the bash tool: %v", err)
	}
	write, err := NewWriteTool(root)
	if err != nil {
		t.Fatalf("building the write tool: %v", err)
	}
	const parentID = "turn-parent"
	base := Config{
		Model:          &stubModel{decisions: decisions},
		Spend:          SpendAPIKey,
		Tools:          NewRegistry(write, bash),
		Caps:           Caps{MaxSteps: 20},
		ResultBytesCap: 4096,
		ArtifactDir:    filepath.Join(root, "artifacts"),
		NewID:          func() string { return parentID },
	}
	spawn := NewSpawnTool(parentID, base, &subagent.Roster{})
	parent := base
	parent.Task = "hand the work to a child"
	parent.Tools = NewRegistry(write, bash, spawn)
	return parent, spawn
}

func scratchTreeWithTwoOwners(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{"mine", "theirs"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "theirs", "notes.txt"), []byte("somebody else's file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestAChildShellWritingOutsideItsPathsIsRefusedWithNoGateInPlace(t *testing.T) {
	root := scratchTreeWithTwoOwners(t)
	parent, spawn := parentTurnWithAShell(t, root, []llm.Decision{
		spawnCall("call-1", "stay inside mine/", "mine/**"),
		bashCall("call-2", "echo the child reached outside > theirs/notes.txt"),
		messageDecision(),
		messageDecision(),
	})
	if parent.Gate != nil {
		t.Fatal("this run is the gate-off case and the config carries a gate")
	}

	row, err := Run(context.Background(), parent)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}

	kept, err := os.ReadFile(filepath.Join(root, "theirs", "notes.txt"))
	if err != nil {
		t.Fatalf("reading the file the child was told to leave alone: %v", err)
	}
	if string(kept) != "somebody else's file\n" {
		t.Fatalf("the shell reached outside the child's paths and rewrote the file as %q", kept)
	}

	child := spawn.Children()[0]
	refusal := firstToolCall(t, child).Error
	for _, want := range []string{"bash", "theirs/notes.txt", "outside the paths this agent holds", "mine/**"} {
		if !strings.Contains(refusal, want) {
			t.Fatalf("the refusal does not name %q: %q", want, refusal)
		}
	}
	showRow(t, "child refused at the shell", child)
	showRow(t, "parent", row)
}

func TestAChildShellInsideItsOwnPathsStillRuns(t *testing.T) {
	root := scratchTreeWithTwoOwners(t)
	parent, spawn := parentTurnWithAShell(t, root, []llm.Decision{
		spawnCall("call-1", "stay inside mine/", "mine/**"),
		bashCall("call-2", "echo written by the child shell > mine/notes.txt"),
		messageDecision(),
		messageDecision(),
	})

	if _, err := Run(context.Background(), parent); err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}

	written, err := os.ReadFile(filepath.Join(root, "mine", "notes.txt"))
	if err != nil {
		t.Fatalf("the child's own shell did not do the work: %v", err)
	}
	if string(written) != "written by the child shell\n" {
		t.Fatalf("the child shell wrote %q", written)
	}
	child := spawn.Children()[0]
	call := firstToolCall(t, child)
	if call.Error != "" || call.ExitCode == nil || *call.ExitCode != 0 {
		t.Fatalf("the command inside the child's own paths was not allowed to run: %+v", call)
	}
	showRow(t, "child allowed at the shell", child)
}

func TestAParentShellIsNotHeldToAnyOwnsBecauseItHoldsTheWholeTree(t *testing.T) {
	root := scratchTreeWithTwoOwners(t)
	parent, _ := parentTurnWithAShell(t, root, []llm.Decision{
		bashCall("call-1", "echo the parent owns the tree > theirs/notes.txt"),
		messageDecision(),
	})

	row, err := Run(context.Background(), parent)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if call := firstToolCall(t, row); call.Error != "" {
		t.Fatalf("the parent's own shell was refused: %+v", call)
	}
	written, err := os.ReadFile(filepath.Join(root, "theirs", "notes.txt"))
	if err != nil || string(written) != "the parent owns the tree\n" {
		t.Fatalf("the parent's shell did not write: %q %v", written, err)
	}
}

func shellSaysAboutItself(t *testing.T, tool *BashTool) string {
	t.Helper()
	cmd := exec.Command(tool.shell, "-c", "uname -o")
	cmd.Dir = string(tool.root)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("asking the shell what it is: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func TestTheShellDescriptionNamesTheFamilyButNoDirectory(t *testing.T) {
	root := t.TempDir()
	tool, err := NewBashTool(root)
	if err != nil {
		t.Fatalf("building the bash tool: %v", err)
	}
	family := shellSaysAboutItself(t, tool)
	description := tool.Definition().Description

	if !strings.Contains(description, family) {
		t.Fatalf("the description does not name %q: %q", family, description)
	}
	if strings.Contains(description, filepath.ToSlash(root)) || strings.Contains(description, root) {
		t.Fatalf("the description carries the working directory %q: %q", root, description)
	}
}

func shellSpelledWorkingDirectory(t *testing.T, tool *BashTool) string {
	t.Helper()
	cmd := exec.Command(tool.shell, "-c", "pwd")
	cmd.Dir = string(tool.root)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("asking the shell for its own working directory: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func TestNoToolDefinitionCarriesTheAbsoluteWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	bash, err := NewBashTool(root)
	if err != nil {
		t.Fatalf("building the bash tool: %v", err)
	}
	spelled := shellSpelledWorkingDirectory(t, bash)
	registry := NewRegistry(bash)
	for _, def := range registry.Definitions() {
		for _, spelling := range []string{root, filepath.ToSlash(root), spelled} {
			if strings.Contains(def.Description, spelling) {
				t.Fatalf("tool %s carries the absolute working directory %q in its description: %q", def.Name, spelling, def.Description)
			}
		}
	}
}

func TestTheShellDescriptionIsIdenticalFromTwoDifferentWorkingDirectories(t *testing.T) {
	var descriptions []string
	for range 2 {
		tool, err := NewBashTool(t.TempDir())
		if err != nil {
			t.Fatalf("building the bash tool: %v", err)
		}
		descriptions = append(descriptions, tool.Definition().Description)
	}
	if descriptions[0] != descriptions[1] {
		t.Fatalf("two tools in different directories describe themselves differently:\n%q\n%q", descriptions[0], descriptions[1])
	}
}

func TestACommandThatFailsSaysSoWithItsExitCode(t *testing.T) {
	tool, err := NewBashTool(t.TempDir())
	if err != nil {
		t.Fatalf("building the bash tool: %v", err)
	}
	result, err := tool.Run(context.Background(), json.RawMessage(`{"command":"echo the first half ran; exit 3"}`))
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if result.ExitCode == nil || *result.ExitCode != 3 {
		t.Fatalf("the exit code is %v", result.ExitCode)
	}
	if !strings.Contains(result.Content, "the first half ran") {
		t.Fatalf("the output the command did produce is gone: %q", result.Content)
	}
	if !strings.Contains(result.Content, "exited 3") {
		t.Fatalf("the result does not say the command failed or with what code: %q", result.Content)
	}
}
