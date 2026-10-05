package shell

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestARestartedShellSaysItIsAnAgentNeverWaitsOnAGitEditorAndHoldsNoKey(t *testing.T) {
	t.Setenv("OPENROUTER_KEY", "sk-or-not-a-real-key")
	t.Setenv("AI_AGENT", "")
	t.Setenv("GIT_TERMINAL_PROMPT", "")
	t.Setenv("GCM_INTERACTIVE", "")
	t.Setenv("EDITOR", "vim")
	t.Setenv("GIT_EDITOR", "code --wait")
	t.Setenv("GIT_SEQUENCE_EDITOR", "code --wait")
	registry := OpenAt(filepath.Join(t.TempDir(), "shells"))
	if _, err := registry.Start(t.TempDir(), "env", "env", ""); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if ran, _ := registry.Read("env"); ran.State != Running {
			break
		}
	}
	printed, err := registry.Tail("env", DefaultTail)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.ReplaceAll(printed, "\r", ""), "\n")
	for _, want := range []string{"AI_AGENT=tofu", "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never", "EDITOR=vim", "GIT_EDITOR=true", "GIT_SEQUENCE_EDITOR=true"} {
		if !slices.Contains(lines, want) {
			t.Errorf("%s is missing from the restarted shell", want)
		}
	}
	for _, line := range lines {
		if strings.Contains(line, "code --wait") || strings.HasPrefix(strings.ToUpper(line), "OPENROUTER_KEY=") {
			t.Errorf("the restarted shell holds %q", line)
		}
	}
}
