package rule

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"boji/internal/sys"
)

func writeCommentFixtureTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	slash := string(rune('/')) + string(rune('/'))
	src := "package p\n" +
		slash + " a line comment\n" +
		"func Exported() {}\n" +
		"var x = 1 " + slash + " trailing\n"
	if err := os.WriteFile(filepath.Join(dir, "fixture.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	return dir
}

func TestCommentsCheckerMatchesBojiLintCommentsLineByLine(t *testing.T) {
	root := writeCommentFixtureTree(t)
	want, err := sys.TreeCommentViolations(root)
	if err != nil {
		t.Fatalf("sys.TreeCommentViolations: %v", err)
	}
	if len(want) == 0 {
		t.Fatal("the comparison tree has no known comment violations to check against")
	}
	wantLines := make([]string, len(want))
	for i, c := range want {
		wantLines[i] = fmt.Sprintf("%s:%d:%d", c.File, c.Line, c.Column)
	}
	sort.Strings(wantLines)

	files, err := sys.ListFiles(root, ".go")
	if err != nil {
		t.Fatalf("sys.ListFiles: %v", err)
	}
	r := Rule{ID: "comments", Kind: KindStructural, Checker: "comments", Mode: ModeShadow}
	var gotLines []string
	for _, name := range files {
		full := filepath.Join(root, name)
		fire, err := Run(r, Builtins(), GoFile{Path: full}, full, time.Now())
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		for _, f := range fire.Findings {
			gotLines = append(gotLines, f.Target)
		}
	}
	sort.Strings(gotLines)

	if len(gotLines) != len(wantLines) {
		t.Fatalf("the registry found %d violations, boji lint comments found %d over the same tree:\nregistry: %v\nlint:     %v", len(gotLines), len(wantLines), gotLines, wantLines)
	}
	for i := range wantLines {
		if gotLines[i] != wantLines[i] {
			t.Fatalf("line %d: registry says %q, boji lint comments says %q", i, gotLines[i], wantLines[i])
		}
	}
}

func TestNoWorktreeFiresOnParsedArgvNotOnTheTextOfAHeredocBody(t *testing.T) {
	r := Rule{ID: "no_worktree", Kind: KindStructural, Checker: "no_worktree", Mode: ModeShadow}

	documentAboutTheRule := ShellCommand{
		Argv:          []string{"git", "commit", "-m", "add the ticket"},
		HeredocBodies: []string{"a hook refuses git worktree, so parallelism is disjoint ownership instead"},
	}
	fire, err := Run(r, Builtins(), documentAboutTheRule, "commit-with-heredoc", time.Now())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(fire.Findings) != 0 {
		t.Fatalf("the rule fired on heredoc text that only mentions worktree, findings: %+v", fire.Findings)
	}

	actualCommand := ShellCommand{Argv: []string{"git", "worktree", "add", "../x"}}
	fire, err = Run(r, Builtins(), actualCommand, "worktree-add", time.Now())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(fire.Findings) != 1 {
		t.Fatalf("the rule did not fire on an actual git worktree invocation, findings: %+v", fire.Findings)
	}
}
