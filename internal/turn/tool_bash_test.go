package turn

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/shell"
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

func TestAnEmptySettingResolvesExactlyAsTheEnvironmentVariableDid(t *testing.T) {
	env := fakeShellEnv()
	env.lookPath = func(name string) (string, error) {
		if name == "git" {
			return `C:\Program Files\Git\cmd\git.exe`, nil
		}
		return "", os.ErrNotExist
	}
	env.stat = func(path string) error {
		if path == `C:\Program Files\Git\bin\bash.exe` {
			return nil
		}
		return os.ErrNotExist
	}
	env.probe = func(string) (string, error) { return "Msys", nil }
	env.setting = ""

	choice, err := resolveShell(env)
	if err != nil {
		t.Fatalf("resolving the shell: %v", err)
	}
	if choice.Path != `C:\Program Files\Git\bin\bash.exe` {
		t.Fatalf("an empty setting changed the resolution: %+v", choice)
	}
}

func TestTheShellSettingWinsOverTOFU_SHELLWhenBothAreSet(t *testing.T) {
	env := fakeShellEnv()
	env.stat = func(path string) error {
		if path == `D:\setting\shell.exe` || path == `D:\envvar\shell.exe` {
			return nil
		}
		return os.ErrNotExist
	}
	env.probe = func(string) (string, error) { return "Msys", nil }
	env.getenv = func(name string) string {
		if name == "TOFU_SHELL" {
			return `D:\envvar\shell.exe`
		}
		return ""
	}
	env.setting = `D:\setting\shell.exe`

	choice, err := resolveShell(env)
	if err != nil {
		t.Fatalf("resolving the shell: %v", err)
	}
	if choice.Path != `D:\setting\shell.exe` {
		t.Fatalf("TOFU_SHELL won over the shell setting: %+v", choice)
	}
}

func TestTheShellResolvesOnceAcrossARunThatBuildsBothTheEnvironmentAndTheToolRegistry(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "go.mod"), "module x\n")
	var resolves int
	env := fakeShellEnv()
	env.goos = "linux"
	env.stat = func(string) error { return nil }
	env.probe = func(string) (string, error) { resolves++; return "Linux", nil }

	shell, err := resolveRunShell(env, realToolchainRunner)
	if err != nil {
		t.Fatalf("resolving the run shell: %v", err)
	}
	tool, err := NewBashToolFromShell(root, shell)
	if err != nil {
		t.Fatalf("building the bash tool: %v", err)
	}
	tool.Definition()
	_ = EnvironmentFromShell(root, time.Now(), shell)

	if resolves != 1 {
		t.Fatalf("the shell resolved %d times across one run, wanted 1", resolves)
	}
}

func TestOneToolchainCacheIsSharedByTheBashToolAndTheEnvironmentBlock(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "go.mod"), "module x\n")
	var probes int
	run := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		probes++
		return []byte("go1.99.0\n"), nil
	}
	env := fakeShellEnv()
	env.goos = "linux"
	env.stat = func(string) error { return nil }
	env.probe = func(string) (string, error) { return "Linux", nil }

	shell, err := resolveRunShell(env, run)
	if err != nil {
		t.Fatalf("resolving the run shell: %v", err)
	}
	tool, err := NewBashToolFromShell(root, shell)
	if err != nil {
		t.Fatalf("building the bash tool: %v", err)
	}
	tool.Definition()
	block := EnvironmentFromShell(root, time.Now(), shell)
	if !strings.Contains(block, "go go1.99.0") {
		t.Fatalf("the environment block never names the probed toolchain: %q", block)
	}

	if probes != 1 {
		t.Fatalf("the tool and the environment block probed the toolchain %d times across one run, wanted 1", probes)
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

	tool, err := newBashToolWithDeps(root, env, run, nil)
	if err != nil {
		t.Fatalf("building the bash tool: %v", err)
	}
	tool.Definition()
	if calls != 1 {
		t.Fatalf("building the tool and reading its definition probed the toolchain %d times, wanted 1", calls)
	}
	for range 3 {
		tool.Definition()
	}
	if calls != 1 {
		t.Fatalf("the probe ran %d times across a three step turn, wanted 1", calls)
	}
}

func TestTheProbeStartsAtConstructionAndIsOnlyWaitedOnWhenTheDefinitionNeedsIt(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "go.mod"), "module x\n")
	release := make(chan struct{})
	started := make(chan struct{})
	var once sync.Once
	run := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		once.Do(func() { close(started) })
		<-release
		return []byte("go1.99.0\n"), nil
	}
	env := fakeShellEnv()
	env.goos = "linux"
	env.stat = func(string) error { return nil }
	env.probe = func(string) (string, error) { return "Linux", nil }

	before := time.Now()
	tool, err := newBashToolWithDeps(root, env, run, nil)
	if err != nil {
		t.Fatalf("building the bash tool: %v", err)
	}
	if constructing := time.Since(before); constructing > 200*time.Millisecond {
		t.Fatalf("construction waited on the probe instead of overlapping it: %v", constructing)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("the probe never started")
	}

	definitionReturned := make(chan string, 1)
	go func() { definitionReturned <- tool.Definition().Description }()
	select {
	case <-definitionReturned:
		t.Fatal("Definition returned before the probe it depends on had finished")
	case <-time.After(150 * time.Millisecond):
	}

	close(release)
	description := <-definitionReturned
	if !strings.Contains(description, "go go1.99.0") {
		t.Fatalf("the description never carries the resolved toolchain once it waited for the probe: %q", description)
	}
}

func TestAnUnfinishedProbeNeverReportsAnInterpreterAsMissing(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "go.mod"), "module x\n")
	release := make(chan struct{})
	run := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		<-release
		return []byte("go1.99.0\n"), nil
	}
	env := fakeShellEnv()
	env.goos = "linux"
	env.stat = func(string) error { return nil }
	env.probe = func(string) (string, error) { return "Linux", nil }

	tool, err := newBashToolWithDeps(root, env, run, nil)
	if err != nil {
		t.Fatalf("building the bash tool: %v", err)
	}
	args, err := json.Marshal(bashArgs{Command: "go build ./..."})
	if err != nil {
		t.Fatal(err)
	}
	runReturned := make(chan error, 1)
	go func() {
		_, runErr := tool.Run(context.Background(), args)
		runReturned <- runErr
	}()

	select {
	case runErr := <-runReturned:
		t.Fatalf("go was answered before its probe finished, with %v: an answer given while unfinished can only be a guess, and here it would have guessed missing", runErr)
	case <-time.After(150 * time.Millisecond):
	}

	close(release)
	if runErr := <-runReturned; runErr != nil && strings.Contains(runErr.Error(), "not on this shell's PATH") {
		t.Fatalf("go was really present and the probe said so once it finished, but the command was still refused as missing: %v", runErr)
	}
}

func TestASecondBashToolInTheSameDirectoryAfterTheWiringReusesTheRunCache(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns the real shell and probes the real toolchain")
	}
	root, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	shell, err := ResolveRunShell("")
	if err != nil {
		t.Fatalf("resolving the run shell on this machine: %v", err)
	}

	first := time.Now()
	tool1, err := NewBashToolFromShell(root, shell)
	if err != nil {
		t.Fatalf("building the first bash tool: %v", err)
	}
	tool1.Definition()
	firstElapsed := time.Since(first)

	second := time.Now()
	tool2, err := NewBashToolFromShell(root, shell)
	if err != nil {
		t.Fatalf("building the second bash tool: %v", err)
	}
	tool2.Definition()
	secondElapsed := time.Since(second)

	t.Logf("first construction+definition (probe pays here): %v, second in the same directory with the shared cache: %v", firstElapsed, secondElapsed)
}

func TestAToolchainCacheAnswersASecondBashToolInTheSameDirectoryWithoutProbingAgain(t *testing.T) {
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
	cache := NewToolchainCache()

	first, err := newBashToolWithDeps(root, env, run, cache)
	if err != nil {
		t.Fatalf("building the first bash tool: %v", err)
	}
	first.Definition()

	second, err := newBashToolWithDeps(root, env, run, cache)
	if err != nil {
		t.Fatalf("building the second bash tool: %v", err)
	}
	second.Definition()

	if calls != 1 {
		t.Fatalf("a second bash tool in the same directory probed the toolchain %d times, wanted 1", calls)
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

	tool, err := newBashToolWithDeps(root, env, absent, nil)
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

func TestAKnownExitCodeRecordsWhyRatherThanJustTheNumber(t *testing.T) {
	tool, err := NewBashTool(t.TempDir())
	if err != nil {
		t.Fatalf("building the bash tool: %v", err)
	}
	cases := []struct {
		command string
		want    string
	}{
		{"exit 127", "not found in this environment"},
		{"exit 126", "found but not executable"},
	}
	for _, c := range cases {
		args, err := json.Marshal(bashArgs{Command: c.command})
		if err != nil {
			t.Fatal(err)
		}
		result, err := tool.Run(context.Background(), args)
		if err != nil {
			t.Fatalf("%s: Run returned an error: %v", c.command, err)
		}
		if !strings.Contains(result.FailureText, c.want) {
			t.Fatalf("%s: FailureText is %q, want it to say %q", c.command, result.FailureText, c.want)
		}
	}
}

func waitForRunningRow(t *testing.T, registry *shell.Registry, deadline time.Duration) shell.Shell {
	t.Helper()
	stop := time.Now().Add(deadline)
	for time.Now().Before(stop) {
		found, err := registry.List()
		if err == nil {
			for _, one := range found {
				if one.State == shell.Running {
					return one
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("no running row appeared in the registry within %s", deadline)
	return shell.Shell{}
}

func TestASlowCommandThroughTheBashToolRegistersAsARunningShell(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns the real shell for several seconds")
	}
	root := t.TempDir()
	tool, err := NewBashTool(root)
	if err != nil {
		t.Fatalf("building the bash tool: %v", err)
	}
	registry := shell.OpenAt(filepath.Join(root, "shells"))
	args, err := json.Marshal(bashArgs{Command: "sleep 5", TimeoutMS: 8000})
	if err != nil {
		t.Fatal(err)
	}
	resultCh := make(chan Result, 1)
	go func() {
		result, runErr := tool.Run(WithShellRegistry(context.Background(), registry), args)
		if runErr != nil {
			t.Errorf("running the command: %v", runErr)
		}
		resultCh <- result
	}()

	row := waitForRunningRow(t, registry, 6*time.Second)
	if row.Command != "sleep 5" {
		t.Fatalf("the running row carries command %q, want %q", row.Command, "sleep 5")
	}
	if row.Started.IsZero() {
		t.Fatalf("the running row carries no start time")
	}

	result := <-resultCh
	if result.ExitCode == nil || *result.ExitCode != 0 {
		t.Fatalf("the finished result carries exit code %v, want 0", result.ExitCode)
	}
}

func TestARunningRowLeavesWithTheCommandsExitCode(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns the real shell for several seconds")
	}
	root := t.TempDir()
	tool, err := NewBashTool(root)
	if err != nil {
		t.Fatalf("building the bash tool: %v", err)
	}
	registry := shell.OpenAt(filepath.Join(root, "shells"))
	args, err := json.Marshal(bashArgs{Command: "sleep 4; exit 9", TimeoutMS: 8000})
	if err != nil {
		t.Fatal(err)
	}
	go func() { _, _ = tool.Run(WithShellRegistry(context.Background(), registry), args) }()
	row := waitForRunningRow(t, registry, 6*time.Second)

	deadline := time.Now().Add(10 * time.Second)
	for {
		entry, readErr := registry.Read(row.Name)
		if readErr != nil {
			t.Fatalf("reading the row back: %v", readErr)
		}
		if entry.State == shell.Exited {
			if entry.ExitCode == nil || *entry.ExitCode != 9 {
				t.Fatalf("the exited row carries exit code %v, want 9", entry.ExitCode)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the row never left the running state")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestAKilledBashCommandLeavesTheRegistryRowKilled(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns the real shell for several seconds")
	}
	root := t.TempDir()
	tool, err := NewBashTool(root)
	if err != nil {
		t.Fatalf("building the bash tool: %v", err)
	}
	registry := shell.OpenAt(filepath.Join(root, "shells"))
	args, err := json.Marshal(bashArgs{Command: "sleep 30", TimeoutMS: 60000})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		_, _ = tool.Run(WithShellRegistry(context.Background(), registry), args)
		close(done)
	}()

	row := waitForRunningRow(t, registry, 6*time.Second)
	if err := registry.Kill(row.Name); err != nil {
		t.Fatalf("killing the running row: %v", err)
	}
	entry, err := registry.Read(row.Name)
	if err != nil {
		t.Fatalf("reading the row back: %v", err)
	}
	if entry.State != shell.Killed {
		t.Fatalf("the row reads as %q right after Kill, want %q", entry.State, shell.Killed)
	}

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the bash tool never returned after its process was killed")
	}
	afterExit, err := registry.Read(row.Name)
	if err != nil {
		t.Fatalf("reading the row after the tool returned: %v", err)
	}
	if afterExit.State != shell.Killed {
		t.Fatalf("the bash tool's own exit overwrote a kill with %q", afterExit.State)
	}
}

func TestAToolResultIsByteIdenticalWithAndWithoutAShellRegistry(t *testing.T) {
	root := t.TempDir()
	tool, err := NewBashTool(root)
	if err != nil {
		t.Fatalf("building the bash tool: %v", err)
	}
	args, err := json.Marshal(bashArgs{Command: "echo the same either way"})
	if err != nil {
		t.Fatal(err)
	}
	without, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("running without a registry: %v", err)
	}
	registry := shell.OpenAt(filepath.Join(root, "shells"))
	with, err := tool.Run(WithShellRegistry(context.Background(), registry), args)
	if err != nil {
		t.Fatalf("running with a registry attached: %v", err)
	}
	if without.Content != with.Content || *without.ExitCode != *with.ExitCode || without.Command != with.Command || without.FailureText != with.FailureText {
		t.Fatalf("a shell registry in the context changed what the model receives:\nwithout: %+v\nwith:    %+v", without, with)
	}
}

func TestABackgroundCommandReturnsAHandleInsideASecondAndTheTurnContinues(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a real process that outlives the call")
	}
	root := t.TempDir()
	tool, err := NewBashTool(root)
	if err != nil {
		t.Fatalf("building the bash tool: %v", err)
	}
	defer tool.StopBackground()
	registry := shell.OpenAt(filepath.Join(root, "shells"))
	args, err := json.Marshal(bashArgs{Command: "sleep 30", Background: true})
	if err != nil {
		t.Fatal(err)
	}

	started := time.Now()
	result, runErr := tool.Run(WithShellRegistry(context.Background(), registry), args)
	took := time.Since(started)

	if runErr != nil {
		t.Fatalf("starting a background command: %v", runErr)
	}
	if took > time.Second {
		t.Fatalf("getting a handle back took %v, want under a second", took)
	}
	if !strings.Contains(result.Content, "bash-1") {
		t.Fatalf("the result %q never names the handle the process runs under", result.Content)
	}

	shells, err := registry.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(shells) != 1 || shells[0].State != shell.Running {
		t.Fatalf("registered shells: %+v, want exactly one still running", shells)
	}
}

func TestStoppingBackgroundLeavesNothingRunning(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a real process that outlives the call")
	}
	root := t.TempDir()
	tool, err := NewBashTool(root)
	if err != nil {
		t.Fatalf("building the bash tool: %v", err)
	}
	registry := shell.OpenAt(filepath.Join(root, "shells"))
	args, err := json.Marshal(bashArgs{Command: "sleep 30", Background: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, runErr := tool.Run(WithShellRegistry(context.Background(), registry), args); runErr != nil {
		t.Fatalf("starting a background command: %v", runErr)
	}

	tool.StopBackground()

	deadline := time.Now().Add(5 * time.Second)
	for {
		shells, listErr := registry.List()
		if listErr != nil {
			t.Fatal(listErr)
		}
		if len(shells) == 1 && shells[0].State == shell.Killed {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the turn ended and the background process is still %+v", shells)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestAResultOverTheCapIsCutOnARuneBoundaryAndNamesWhatWasDropped(t *testing.T) {
	head := strings.Repeat("h", 100)
	multiByte := strings.Repeat("é", 50)
	tail := strings.Repeat("t", 100)
	content := head + multiByte + tail
	capBytes := 50

	cut := capResult(content, capBytes)

	if !utf8.ValidString(cut) {
		t.Fatalf("the cap sliced a multi-byte rune in half: %q", cut)
	}
	if !strings.HasPrefix(cut, "hhhhh") {
		t.Fatalf("the head is gone from the cut result: %q", cut)
	}
	if !strings.HasSuffix(cut, "ttttt") {
		t.Fatalf("the tail is gone from the cut result: %q", cut)
	}
	if !strings.Contains(cut, "dropped from the middle") || !strings.Contains(cut, "byte cap") {
		t.Fatalf("the cut result carries no marker naming what was dropped: %q", cut)
	}
	if len(cut) > capBytes+len(fmt.Sprintf(resultCapMarker, "999 bytes", capBytes)) {
		t.Fatalf("the cut result is %d bytes, nowhere near the %d byte cap", len(cut), capBytes)
	}
}

func TestACancelledBashCallIsRecordedAsAbortedRatherThanFailed(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns the real shell for several seconds")
	}
	root := t.TempDir()
	tool, err := NewBashTool(root)
	if err != nil {
		t.Fatalf("building the bash tool: %v", err)
	}
	args, err := json.Marshal(bashArgs{Command: "sleep 30", TimeoutMS: 60000})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(300 * time.Millisecond)
		cancel()
	}()

	result, err := tool.Run(ctx, args)
	if err != nil {
		t.Fatalf("a cancelled call returned a go error instead of a result carrying its own outcome: %v", err)
	}
	if result.Outcome != ResultAborted {
		t.Fatalf("Outcome = %v, want ResultAborted, not folded into an ordinary failure", result.Outcome)
	}
	if !strings.Contains(result.FailureText, "cancelled") {
		t.Fatalf("FailureText %q never says the call was cancelled rather than failed on its own", result.FailureText)
	}

	failed, err := tool.Run(context.Background(), json.RawMessage(`{"command":"exit 3"}`))
	if err != nil {
		t.Fatalf("running a command that exits nonzero: %v", err)
	}
	if failed.Outcome != ResultFailed {
		t.Fatalf("an ordinary nonzero exit carries Outcome %v, want ResultFailed", failed.Outcome)
	}
}

func TestARowBuiltFromAnAbortedResultNamesTheAbortRatherThanReadingLikeAFailure(t *testing.T) {
	tool := &stubTool{name: "read", result: Result{Outcome: ResultAborted, FailureText: "cancelled elsewhere in this turn"}}
	model := &stubModel{decisions: []llm.Decision{
		toolCallDecision(llm.ToolCall{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{}`)}),
		messageDecision(),
	}}

	row, err := Run(context.Background(), baseConfig(t, model, NewRegistry(tool)))
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}

	call := firstToolCall(t, row)
	if !strings.HasPrefix(call.Error, "aborted:") {
		t.Fatalf("row.Error = %q, want it to open by saying the call was aborted rather than reading as a bare failure", call.Error)
	}
	if call.Outcome() != llm.ToolOutcomeFailed {
		t.Fatalf("an aborted call still has to read as not-ran on the wire, got %v", call.Outcome())
	}
	last := row.Steps[len(row.Steps)-1]
	if last.AssistantText != "done" {
		t.Fatalf("the turn did not go on past the aborted call: %+v", last)
	}
}

func TestCheckPortTellsListeningFromNotWithoutAnHTTPRequest(t *testing.T) {
	root := t.TempDir()
	tool, err := NewBashTool(root)
	if err != nil {
		t.Fatalf("building the bash tool: %v", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	port := listener.Addr().(*net.TCPAddr).Port

	args, err := json.Marshal(bashArgs{CheckPort: port})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	result, err := tool.Run(context.Background(), args)
	took := time.Since(started)
	if err != nil {
		t.Fatalf("checking a listening port: %v", err)
	}
	if !strings.Contains(result.Content, "is listening") {
		t.Fatalf("checking a listening port returned %q", result.Content)
	}
	if took > 50*time.Millisecond {
		t.Fatalf("checking a local port took %v, want well under the %dms timeout", took, konst.PortCheckTimeoutMillis)
	}
	t.Logf("check_port on a listening port took %v", took)

	_ = listener.Close()
	args, err = json.Marshal(bashArgs{CheckPort: port})
	if err != nil {
		t.Fatal(err)
	}
	result, err = tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("checking a closed port: %v", err)
	}
	if !strings.Contains(result.Content, "not listening") {
		t.Fatalf("checking a closed port returned %q", result.Content)
	}
}
