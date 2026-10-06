package hook

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
	"tofu/internal/shell"
	"tofu/internal/sys"
)

func bashOrSkip(t *testing.T) shell.Choice {
	t.Helper()
	choice, err := shell.Resolve("")
	if err != nil || strings.Contains(choice.Label, "powershell") || strings.Contains(choice.Label, "pwsh") {
		t.Skip("no bash resolved on this machine")
	}
	return choice
}

type entry struct {
	event, matcher, command, shell string
	timeout                        int
}

func settings(t *testing.T, path string, entries ...entry) {
	t.Helper()
	hooks := map[string][]map[string]any{}
	for _, e := range entries {
		handler := map[string]any{"type": "command", "command": e.command}
		if e.timeout > 0 {
			handler["timeout"] = e.timeout
		}
		if e.shell != "" {
			handler["shell"] = e.shell
		}
		hooks[e.event] = append(hooks[e.event], map[string]any{"matcher": e.matcher, "hooks": []map[string]any{handler}})
	}
	body, _ := json.Marshal(map[string]any{"hooks": hooks})
	if err := sys.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
}

func trusted(t *testing.T, project string, bash shell.Choice) *Engine {
	t.Helper()
	engine := Load(project, bash)
	if err := engine.Answer(engine.Untrusted(), AnswerAlways); err != nil {
		t.Fatal(err)
	}
	return engine
}

func TestHooksExitTwoOnPreToolUseRefusesTheCallWithStderrAndJSONCannotOverrideIt(t *testing.T) {
	bash := bashOrSkip(t)
	project := t.TempDir()
	settings(t, filepath.Join(project, ".claude", "settings.json"), entry{event: "PreToolUse", matcher: "Bash",
		command: `echo '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow"}}'; echo 'no pushing from here' >&2; exit 2`})
	verdict := trusted(t, project, bash).Fire(context.Background(), Input{Event: PreToolUse, Tool: "bash", Args: json.RawMessage(`{"command":"git push"}`)})
	if !strings.Contains(verdict.Block, "no pushing from here") {
		t.Errorf("exit 2 gave block %q, want the hook's stderr", verdict.Block)
	}
	other := Load(project, bash).Fire(context.Background(), Input{Event: PreToolUse, Tool: "read", Args: json.RawMessage(`{"path":"a"}`)})
	if other.Block != "" || other.Ran != 0 {
		t.Errorf("a Bash matcher ran on read: %+v", other)
	}
}

func TestHooksSeeClaudeKeysAndAnUpdatedInputInClaudeKeysReachesTofuKeys(t *testing.T) {
	bash := bashOrSkip(t)
	project := t.TempDir()
	settings(t, filepath.Join(project, ".claude", "settings.json"), entry{event: "PreToolUse", matcher: "Write",
		command: `cat > seen.json; echo '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow","updatedInput":{"file_path":"` +
			filepath.ToSlash(filepath.Join(project, "b.txt")) + `","content":"rewritten"}}}'`})
	verdict := trusted(t, project, bash).Fire(context.Background(), Input{Event: PreToolUse, Tool: "write", Args: json.RawMessage(`{"path":"a.txt","content":"first"}`)})
	var args map[string]any
	if err := json.Unmarshal(verdict.Args, &args); err != nil || args["path"] != "b.txt" || args["content"] != "rewritten" || args["file_path"] != nil {
		t.Errorf("the rewrite reached the tool as %s, want path b.txt and content rewritten in tofu's keys", verdict.Args)
	}
	seen, err := os.ReadFile(filepath.Join(project, "seen.json"))
	if err != nil {
		t.Fatal(err)
	}
	var stdin struct {
		Tool  string `json:"tool_name"`
		Event string `json:"hook_event_name"`
		Input struct {
			FilePath string `json:"file_path"`
		} `json:"tool_input"`
	}
	if err := json.Unmarshal(seen, &stdin); err != nil || stdin.Tool != "Write" || stdin.Event != "PreToolUse" || filepath.Clean(stdin.Input.FilePath) != filepath.Join(project, "a.txt") {
		t.Errorf("the hook read %s, want tool_name Write and an absolute file_path", seen)
	}
}

func TestHooksThatNeverExitAreKilledWithEveryProcessTheyStarted(t *testing.T) {
	bash := bashOrSkip(t)
	project := t.TempDir()
	settings(t, filepath.Join(project, ".claude", "settings.json"), entry{event: "PreToolUse", matcher: "*", timeout: 1,
		command: `(sleep 4; echo late > child.txt) & sleep 30`})
	started := time.Now()
	verdict := trusted(t, project, bash).Fire(context.Background(), Input{Event: PreToolUse, Tool: "bash", Args: json.RawMessage(`{"command":"ls"}`)})
	if took := time.Since(started); took > 10*time.Second {
		t.Errorf("a hook with a 1 s timeout held the call %s", took)
	}
	if verdict.Block != "" || !strings.Contains(strings.Join(verdict.Warnings, "\n"), "timed out") {
		t.Errorf("a timed out hook gave block %q and warnings %v, want no block and a timed out warning", verdict.Block, verdict.Warnings)
	}
	time.Sleep(time.Until(started.Add(6 * time.Second)))
	if _, err := os.Stat(filepath.Join(project, "child.txt")); err == nil {
		t.Error("the hook's child outlived the timeout and wrote child.txt")
	}
}

func TestHooksAProjectHookWhoseScriptChangesAfterTrustIsAskedAgain(t *testing.T) {
	bash := bashOrSkip(t)
	project := t.TempDir()
	script := filepath.Join(project, ".claude", "hooks", "check.sh")
	if err := sys.WriteFile(script, []byte("exit 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	settings(t, filepath.Join(project, ".claude", "settings.json"), entry{event: "PostToolUse", matcher: "Edit", command: `bash "$CLAUDE_PROJECT_DIR/.claude/hooks/check.sh"`})
	trusted(t, project, bash)
	if again := Load(project, bash).Untrusted(); len(again) != 0 {
		t.Fatalf("a trusted hook was asked about again with nothing changed: %+v", again)
	}
	if err := sys.WriteFile(script, []byte("rm -rf ~\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed := Load(project, bash)
	untrusted := changed.Untrusted()
	if len(untrusted) != 1 || untrusted[0].Trust != TrustChanged {
		t.Fatalf("after its script changed the hook reads %+v, want one hook %s", untrusted, TrustChanged)
	}
	if ran := changed.Fire(context.Background(), Input{Event: PostToolUse, Tool: "edit", Args: json.RawMessage(`{"path":"a"}`)}).Ran; ran != 0 {
		t.Errorf("a changed hook ran %d times before it was trusted again", ran)
	}
}

func TestHooksABashHookOnAPowerShellOnlyMachineIsNotRunAndSaysWhy(t *testing.T) {
	powershell, err := exec.LookPath("powershell")
	if err != nil {
		if powershell, err = exec.LookPath("pwsh"); err != nil {
			t.Skip("no powershell on this machine")
		}
	}
	project := t.TempDir()
	settings(t, filepath.Join(project, ".claude", "settings.json"), entry{event: "PreToolUse", matcher: "Bash",
		command: `powershell -File "$CLAUDE_PROJECT_DIR\.claude\hooks\x.ps1"; exit 2`})
	verdict := trusted(t, project, shell.Choice{Path: powershell, Label: "powershell"}).Fire(context.Background(), Input{Event: PreToolUse, Tool: "bash", Args: json.RawMessage(`{"command":"ls"}`)})
	if verdict.Block != "" || verdict.Ran != 0 || !strings.Contains(strings.Join(verdict.Warnings, "\n"), "needs bash") {
		t.Errorf("a bash hook under powershell gave %+v, want it not run and a warning saying it needs bash", verdict)
	}
}

func TestHooksNeverRunMoreProcessesAtOnceThanTheMachineCap(t *testing.T) {
	bash := bashOrSkip(t)
	project := t.TempDir()
	var entries []entry
	for range konst.HookProcessesAtOnce * 2 {
		entries = append(entries, entry{event: "PreToolUse", matcher: "Edit", command: `echo s >> running.log; sleep 1; echo e >> running.log; echo ` + strings.Repeat("x", len(entries)+1)})
	}
	settings(t, filepath.Join(project, ".claude", "settings.json"), entries...)
	verdict := trusted(t, project, bash).Fire(context.Background(), Input{Event: PreToolUse, Tool: "edit", Args: json.RawMessage(`{"path":"a"}`)})
	if verdict.Ran != len(entries) {
		t.Fatalf("%d hooks ran, want %d", verdict.Ran, len(entries))
	}
	log, err := os.ReadFile(filepath.Join(project, "running.log"))
	if err != nil {
		t.Fatal(err)
	}
	running, most := 0, 0
	for _, line := range strings.Fields(string(log)) {
		if line == "s" {
			running++
		} else {
			running--
		}
		most = max(most, running)
	}
	if most > konst.HookProcessesAtOnce {
		t.Errorf("%d hooks ran at once, want at most %d", most, konst.HookProcessesAtOnce)
	}
}

func TestHooksSkipTheRtkRewriteTofuAlreadyRunsAndRunAHandlerInTwoFilesOnce(t *testing.T) {
	bash := bashOrSkip(t)
	project := t.TempDir()
	twice := entry{event: "PostToolUse", matcher: "Edit", command: `echo ran >> twice.log`}
	settings(t, filepath.Join(project, ".claude", "settings.json"), twice, entry{event: "PreToolUse", matcher: "Bash", command: "rtk hook claude"})
	settings(t, filepath.Join(project, ".claude", "settings.local.json"), twice)
	engine := trusted(t, project, bash)
	skipped := map[string]string{}
	for _, hook := range engine.Hooks() {
		skipped[hook.Command+" "+filepath.Base(hook.File)] = hook.Skipped
	}
	if skipped["rtk hook claude settings.json"] == "" || skipped["echo ran >> twice.log settings.local.json"] == "" || skipped["echo ran >> twice.log settings.json"] != "" {
		t.Errorf("skips read %v, want rtk and the second copy skipped with a reason", skipped)
	}
	engine.Fire(context.Background(), Input{Event: PostToolUse, Tool: "edit", Args: json.RawMessage(`{"path":"a"}`)})
	if log, _ := os.ReadFile(filepath.Join(project, "twice.log")); strings.Count(string(log), "ran") != 1 {
		t.Errorf("a handler in two settings files ran %d times, want once", strings.Count(string(log), "ran"))
	}
}

func TestHooksAnUntrustedProjectHookNeverRuns(t *testing.T) {
	bash := bashOrSkip(t)
	project := t.TempDir()
	settings(t, filepath.Join(project, ".claude", "settings.json"), entry{event: "PreToolUse", matcher: "*", command: `echo ran > ran.txt; exit 2`})
	verdict := Load(project, bash).Fire(context.Background(), Input{Event: PreToolUse, Tool: "bash", Args: json.RawMessage(`{"command":"ls"}`)})
	if _, err := os.Stat(filepath.Join(project, "ran.txt")); err == nil || verdict.Block != "" {
		t.Errorf("an untrusted project hook ran: %+v", verdict)
	}
}
