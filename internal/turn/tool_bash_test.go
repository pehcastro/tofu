package turn

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"tofu/internal/shell"
)

func runBash(t *testing.T, tool *BashTool, args map[string]any) Result {
	t.Helper()
	raw, _ := json.Marshal(args)
	result, err := tool.Run(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func newBash(t *testing.T) *BashTool {
	t.Helper()
	tool, err := NewBashTool(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return tool
}

func TestBashDeadlineSaysItWasTheDeadline(t *testing.T) {
	alone := runBash(t, newBash(t), map[string]any{"command": "echo partial; sleep 5", "timeout_ms": 1500})
	if !strings.Contains(alone.Content, "partial") || alone.Outcome != ResultFailed || !strings.HasPrefix(alone.FailureText, "bash: hit the deadline after 1.5 s") {
		t.Errorf("no registry: outcome %v, content %q, failure %q", alone.Outcome, alone.Content, alone.FailureText)
	}
	tool := newBash(t)
	registry := shell.OpenAt(t.TempDir())
	ctx := WithShellRegistry(context.Background(), registry)
	raw, _ := json.Marshal(map[string]any{"command": "echo partial; sleep 30", "timeout_ms": 2000})
	listed, err := tool.Run(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(listed.Content, "partial") || listed.Outcome != ResultFailed || !strings.HasPrefix(listed.FailureText, "bash: hit the deadline after 2 s") {
		t.Errorf("registry: outcome %v, content %q, failure %q", listed.Outcome, listed.Content, listed.FailureText)
	}
	shells, _ := registry.List()
	if len(shells) != 1 || shells[0].State != shell.Killed {
		t.Fatalf("the shells screen lists %+v, want the one killed at its deadline", shells)
	}
	if tail, _ := registry.Tail(shells[0].Name, shell.DefaultTail); !strings.HasSuffix(tail, "tofu: hit the deadline after 2 s and was killed") {
		t.Errorf("the shells screen reads %q", tail)
	}
	quick, _ := json.Marshal(map[string]any{"command": "echo quick", "timeout_ms": 5000})
	ended, err := tool.Run(ctx, quick)
	if after, _ := registry.List(); err != nil || ended.Content != "quick\n" || ended.Outcome != ResultSucceeded || len(after) != 1 {
		t.Errorf("a call inside its deadline came back %q (%v), and left %d rows where 1 was", ended.Content, err, len(after))
	}
}

func TestABashCallPastTheSoftLimitReturnsAShellNameAndTheCommandKeepsRunning(t *testing.T) {
	tool := newBash(t)
	tool.softLimit = time.Second
	registry := shell.OpenAt(t.TempDir())
	ctx := WithShellRegistry(context.Background(), registry)
	command := "echo building; sleep 4; echo built > marker"
	raw, _ := json.Marshal(map[string]any{"command": command})
	started := time.Now()
	result, err := tool.Run(ctx, raw)
	took := time.Since(started)
	if err != nil {
		t.Fatal(err)
	}
	name := regexp.MustCompile(`bash-\d+`).FindString(result.Content)
	t.Cleanup(func() { _ = registry.Kill(name) })
	running, readErr := registry.Read(name)
	if name == "" || readErr != nil || running.State != shell.Running || took > 3*time.Second || !strings.Contains(result.Content, "building") {
		t.Fatalf("after %v the call came back %q, shell %q read as %+v (%v), want an early return naming a running shell with the output so far", took, result.Content, name, running, readErr)
	}
	marker := filepath.Join(string(tool.root), "marker")
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the marker exists already, so the command ended before the call returned")
	}
	deadline := time.Now().Add(15 * time.Second)
	for _, err := os.Stat(marker); err != nil; _, err = os.Stat(marker) {
		if time.Now().After(deadline) {
			t.Fatal("the moved command never wrote its marker, so it did not keep running")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestBashHoldsNoKeyTofuReads(t *testing.T) {
	t.Setenv("OPENROUTER_KEY", "sk-or-not-a-real-key")
	t.Setenv("BRAVE_SEARCH_KEY", "not-a-real-key")
	result := runBash(t, newBash(t), map[string]any{"command": "env | grep -ci 'openrouter_key\\|brave_search_key'"})
	if !strings.HasPrefix(result.Content, "0\n") {
		t.Fatalf("the child sees a key: %q", result.Content)
	}
}

func TestBashTellsEveryChildItIsAnAgentForegroundAndBackground(t *testing.T) {
	for _, name := range []string{"AI_AGENT", "GIT_TERMINAL_PROMPT", "GCM_INTERACTIVE", "GIT_EDITOR", "EDITOR", subAgentDepthVar} {
		t.Setenv(name, "")
	}
	tool := newBash(t)
	ctx := WithShellRegistry(context.Background(), shell.OpenAt(t.TempDir()))
	for _, background := range []bool{false, true} {
		raw, _ := json.Marshal(map[string]any{"command": "env", "background": background})
		result, err := tool.Run(ctx, raw)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.ReplaceAll(result.Content, "\r", ""), "\n")
		for _, want := range []string{"AI_AGENT=tofu", "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never", "GIT_EDITOR=true", "GIT_SEQUENCE_EDITOR=true", "EDITOR=false", subAgentDepthVar + "=1"} {
			if !slices.Contains(lines, want) {
				t.Errorf("background %v: %s is missing", background, want)
			}
		}
	}
}

func TestBashBoundsItsBufferAndSaysHowMuchItPrinted(t *testing.T) {
	result := runBash(t, newBash(t), map[string]any{"command": "head -c 3000000 /dev/zero | tr '\\0' a; echo; echo end"})
	if !strings.HasSuffix(result.Content, "end\n") || !strings.Contains(result.Content, strconv.Itoa(3000005)) {
		t.Fatalf("head %q, tail %q", result.Content[:200], result.Content[len(result.Content)-200:])
	}
}

func TestBashDecodesTheConsoleCodePage(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the console code page is a Windows thing")
	}
	result := runBash(t, newBash(t), map[string]any{"command": "printf 'caf\\303\\251\\n'; chcp.com"})
	if !utf8.ValidString(result.Content) || strings.ContainsRune(result.Content, utf8.RuneError) || !strings.HasPrefix(result.Content, "café\n") {
		t.Fatalf("%q", result.Content)
	}
}

func TestPowerShellFallbackSkipsTheProfileAndPrintsUTF8(t *testing.T) {
	path, err := exec.LookPath("powershell")
	if err != nil {
		t.Skip("no powershell on PATH")
	}
	tool := newBash(t)
	tool.choice = shell.Choice{Path: path, Label: "powershell"}
	result := runBash(t, tool, map[string]any{"command": "[Console]::OutputEncoding.CodePage; [Environment]::CommandLine"})
	if !strings.HasPrefix(result.Content, "65001") || !strings.Contains(result.Content, "-NoProfile") {
		t.Fatalf("%q", result.Content)
	}
}
