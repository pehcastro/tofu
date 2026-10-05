package turn

import (
	"context"
	"encoding/json"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"testing"
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

func TestBashTimeoutKeepsTheOutputItHad(t *testing.T) {
	result := runBash(t, newBash(t), map[string]any{"command": "echo partial; sleep 5", "timeout_ms": 2000})
	if !strings.Contains(result.Content, "partial") || result.Outcome == ResultSucceeded || !strings.Contains(result.FailureText, "2000 ms") {
		t.Fatalf("outcome %v, content %q, failure %q", result.Outcome, result.Content, result.FailureText)
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
