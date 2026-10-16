package turn_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"tofu/internal/llm"
	"tofu/internal/subagent"
	"tofu/internal/turn"
	"tofu/internal/turn/tools"
)

func subAgentShellAnswers(t *testing.T, root string, bash *turn.BashTool, commands []string) []string {
	t.Helper()
	brief := "Read the desk sources and report what you find."
	script := &prefetchScript{brief: brief, lead: []llm.Decision{calling("spawn", map[string]any{"task": brief})}}
	for _, command := range commands {
		script.subAgent = append(script.subAgent, calling("bash", map[string]any{"command": command}))
	}
	checked, err := tools.Checked(root, []turn.Tool{bash})
	if err != nil {
		t.Fatal(err)
	}
	base := turn.Config{
		Model:          script,
		Spend:          turn.SpendAPIKey,
		Tools:          turn.NewRegistry(tools.NewMemo().Wrap(checked)...),
		Caps:           turn.Caps{MaxSteps: len(commands) + 4},
		ResultBytesCap: 4096,
		ArtifactDir:    filepath.Join(t.TempDir(), "artifacts"),
		NewID:          func() string { return "turn-orchestrator" },
	}
	spawner := turn.NewSpawnTool("turn-orchestrator", base, &subagent.Roster{})
	orchestrator := base
	orchestrator.Task, orchestrator.Tools, orchestrator.Inbox = "hand the reading to a sub-agent", turn.NewRegistry(spawner), spawner.Inbox
	if err := turn.Lead(context.Background(), orchestrator, nil, nil, func(turn.Row, error) {}); err != nil {
		t.Fatalf("Lead: %v", err)
	}
	if len(script.asked) != len(commands)+1 {
		t.Fatalf("the sub-agent was asked %d times, want %d", len(script.asked), len(commands)+1)
	}
	var answers []string
	for _, asked := range script.asked[1:] {
		answers = append(answers, asked.Messages[len(asked.Messages)-1].Content)
	}
	return answers
}

func TestASubAgentGrepWithABraceRunsUnderAPosixShell(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"apps/desk/crates/ui/src/components/tabs.rs":  "struct Pick { at: usize }\n",
		"apps/desk/crates/plugin/src/manifest.rs":     "pub struct Manifest { name: String }\n",
		"apps/desk/examples/hello.rs":                 "fn main() {}\n",
		"apps/desk/crates/core/schema/tofu.host.json": "{\n  \"AccountStateChanged\": {\n    \"type\": \"object\"\n  }\n}\n",
	}
	for name, body := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	bash, err := turn.NewBashTool(root)
	if err != nil {
		t.Fatal(err)
	}
	if label := bash.Definition().Description; strings.Contains(strings.ToLower(label), "powershell") {
		t.Fatalf("this host resolved PowerShell, so a POSIX shell cannot be driven here: %s", label)
	}
	commands := []string{
		`grep -n "Pick {" apps/desk/crates/ui/src/components/tabs.rs`,
		`grep -rn "Manifest {" apps/desk/crates apps/desk/examples --include=*.rs`,
		`grep -n -A45 "\"AccountStateChanged\": {" apps/desk/crates/core/schema/tofu.host.json`,
	}
	found := []string{"1:struct Pick { at: usize }", "pub struct Manifest { name: String }", `2:  "AccountStateChanged": {`}
	for i, answer := range subAgentShellAnswers(t, root, bash, commands) {
		if !strings.Contains(answer, found[i]) {
			t.Errorf("%s: the grep did not run under %s:\n%s", commands[i], bash.Definition().Description[:60], answer)
		}
	}
}

func TestAPowerShellScriptBlockIsStillRefusedUnderPowerShell(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("PowerShell is resolved as the shell only on Windows, when no POSIX shell is found")
	}
	powershell, err := exec.LookPath("powershell")
	if err != nil {
		t.Skip("no powershell on PATH")
	}
	root := t.TempDir()
	for _, name := range []string{"ProgramFiles", "ProgramFiles(x86)", "LOCALAPPDATA", "GIT_INSTALL_ROOT", "SCOOP", "TOFU_SHELL"} {
		t.Setenv(name, "")
	}
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("PATH", filepath.Dir(powershell))
	shell, err := turn.ResolveRunShell("")
	if err != nil {
		t.Fatal(err)
	}
	bash, err := turn.NewBashToolFromShell(root, shell)
	if err != nil {
		t.Fatal(err)
	}
	if label := bash.Definition().Description; !strings.Contains(label, "powershell") {
		t.Fatalf("the shell resolved is not PowerShell: %s", label)
	}
	answers := subAgentShellAnswers(t, root, bash, []string{"Get-ChildItem | ForEach-Object { Remove-Item $_ }"})
	if !strings.Contains(answers[0], "PowerShell script block") {
		t.Errorf("a PowerShell script block ran under PowerShell:\n%s", answers[0])
	}
}
