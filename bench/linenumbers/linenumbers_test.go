package linenumbers

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"tofu/internal/turn"
)

const sessionsDir = "../../.tofu/sessions"

var numberedLine = regexp.MustCompile(`(?m)^\d+[:\t\x{2192}]`)

func TestNoReadEverEmitsALineNumber(t *testing.T) {
	root := t.TempDir()
	body := "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten\n"
	if err := os.WriteFile(filepath.Join(root, "ten.txt"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	tool, err := turn.NewReadTool(root)
	if err != nil {
		t.Fatalf("building the read tool: %v", err)
	}

	whole, err := tool.Run(context.Background(), json.RawMessage(`{"path":"ten.txt"}`))
	if err != nil {
		t.Fatalf("whole read: %v", err)
	}
	if numberedLine.MatchString(whole.Content) {
		t.Fatalf("a whole read carries a line-numbered line: %q", whole.Content)
	}
	if whole.Content != body {
		t.Fatalf("a whole read changed the bytes it returned: got %q, want %q", whole.Content, body)
	}

	ranged, err := tool.Run(context.Background(), json.RawMessage(`{"path":"ten.txt","start_line":3,"end_line":5}`))
	if err != nil {
		t.Fatalf("ranged read: %v", err)
	}
	if numberedLine.MatchString(ranged.Content) {
		t.Fatalf("a ranged read carries a line-numbered line: %q", ranged.Content)
	}
	if !strings.HasSuffix(ranged.Content, "three\nfour\nfive") {
		t.Fatalf("a ranged read did not carry the raw lines it named: %q", ranged.Content)
	}
}

func TestByteTotalsOverTheRealCorpus(t *testing.T) {
	result, err := Run(sessionsDir)
	if err != nil {
		t.Fatalf("Run(%q): %v", sessionsDir, err)
	}
	if result.Turns == 0 {
		t.Fatal("no turn was read: the sessions directory is empty or the path is wrong")
	}
	if result.ReadCalls == 0 {
		t.Fatal("no read-tool call was counted in the corpus")
	}
	t.Log(Render("test-machine", "2026-09-23", result))
}
