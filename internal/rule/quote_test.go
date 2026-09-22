package rule

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func emDashFindingLines(t *testing.T, path string, except Exception) []string {
	t.Helper()
	r := Rule{ID: "em_dash", Kind: KindStructural, Checker: "em_dash", Mode: ModeShadow, ModeDeclared: true, Except: except}
	fire, err := Run(r, Builtins(), TextFile{Path: path}, path, time.Now())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	targets := make([]string, len(fire.Findings))
	for i, f := range fire.Findings {
		targets[i] = strings.TrimPrefix(f.Target, path)
	}
	return targets
}

func TestTheQuotedExceptionSkipsAFenceAnIndentedBlockAndASpanAndKeepsProseAndABlockquote(t *testing.T) {
	dash := string(rune(0x2014))
	fence := strings.Repeat("`", 3)
	lines := []string{
		"# a document about the rule",
		"ordinary prose " + dash + " an author wrote this",
		"",
		fence,
		"a model refused " + dash + " inside a fence",
		fence,
		"",
		"    an indented block " + dash + " quoted too",
		"",
		"a span `like " + dash + " this` stays a quotation",
		"",
		`a sentence quoting "a refusal ` + dash + ` inside it" keeps the exact text`,
		"",
		"> a blockquote " + dash + " is the author's own choice",
		"",
	}
	path := filepath.Join(t.TempDir(), "quotation.md")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	got := strings.Join(emDashFindingLines(t, path, ExceptionQuoted), " ")
	if got != ":2 :14" {
		t.Fatalf("with the exception on, the rule fired at %q, want the prose line 2 and the blockquote line 14", got)
	}

	naive := emDashFindingLines(t, path, ExceptionNone)
	if len(naive) != 6 {
		t.Fatalf("without the exception the rule fired %d times, want 6, so the fixture no longer distinguishes the two arms: %v", len(naive), naive)
	}
}

func TestTheShippedEmDashRuleDeclaresShadowAndTheQuotedException(t *testing.T) {
	r, err := Load(filepath.Join("..", "..", "library", "general", "rules", "em_dash@1.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !r.ModeDeclared || r.Mode != ModeShadow {
		t.Fatalf("the shipped rule declares mode %q, declared=%t, want an explicit %q", r.Mode, r.ModeDeclared, ModeShadow)
	}
	if r.Except != ExceptionQuoted {
		t.Fatalf("the shipped rule declares except %q, want %q", r.Except, ExceptionQuoted)
	}
}

func TestAnUnknownExceptionValueIsRefusedAtLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.yaml")
	body := "id: x\nkind: structural\nchecker: em_dash\nexcept: whatever\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load accepted an except value no checker understands")
	}
}
