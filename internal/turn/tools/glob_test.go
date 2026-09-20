package tools_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"tofu/internal/konst"
	"tofu/internal/turn"
	"tofu/internal/turn/tools"
)

func globbed(t *testing.T, root, args string) string {
	t.Helper()
	tool, err := tools.NewGlob(root)
	if err != nil {
		t.Fatalf("building the tool: %v", err)
	}
	result, err := tool.Run(context.Background(), json.RawMessage(args))
	if err != nil {
		t.Fatalf("glob %s: %v", args, err)
	}
	return result.Content
}

func lists(content, rel string) bool {
	for _, line := range strings.Split(content, "\n") {
		if line == rel {
			return true
		}
	}
	return false
}

func TestTheRootIgnoreFileKeepsAFileOutOfTheWalk(t *testing.T) {
	root := t.TempDir()
	seed(t, root, ".gitignore", "secret.txt\nbuild/\n/anchored.txt\n")
	seed(t, root, "secret.txt", "x\n")
	seed(t, root, "kept.txt", "x\n")
	seed(t, root, "anchored.txt", "x\n")
	seed(t, root, "deep/anchored.txt", "x\n")
	seed(t, root, "build/out.txt", "x\n")
	seed(t, root, "deep/secret.txt", "x\n")

	listed := globbed(t, root, `{"pattern":"*.txt"}`)
	for _, gone := range []string{"secret.txt", "build/out.txt", "deep/secret.txt", "anchored.txt"} {
		if lists(listed, gone) {
			t.Fatalf("%s is matched by the root .gitignore and was still walked:\n%s", gone, listed)
		}
	}
	for _, kept := range []string{"kept.txt", "deep/anchored.txt"} {
		if !lists(listed, kept) {
			t.Fatalf("%s is not ignored and is missing from the walk:\n%s", kept, listed)
		}
	}
}

func TestAnIgnoreFileInASubdirectoryAppliesToItsSubtreeAndNotAbove(t *testing.T) {
	root := t.TempDir()
	seed(t, root, "notes.log", "x\n")
	seed(t, root, "sub/.gitignore", "*.log\n")
	seed(t, root, "sub/notes.log", "x\n")
	seed(t, root, "sub/deeper/notes.log", "x\n")

	listed := globbed(t, root, `{"pattern":"*.log"}`)
	if !lists(listed, "notes.log") {
		t.Fatalf("the subdirectory .gitignore reached above itself:\n%s", listed)
	}
	if lists(listed, "sub/notes.log") || lists(listed, "sub/deeper/notes.log") {
		t.Fatalf("the subdirectory .gitignore did not apply to its own subtree:\n%s", listed)
	}
}

func TestANegationReIncludesAFileItsParentPatternExcluded(t *testing.T) {
	root := t.TempDir()
	seed(t, root, ".gitignore", "*.env\n!keep.env\n")
	seed(t, root, "secret.env", "x\n")
	seed(t, root, "keep.env", "x\n")

	listed := globbed(t, root, `{"pattern":"*.env"}`)
	if lists(listed, "secret.env") {
		t.Fatalf("*.env did not exclude secret.env:\n%s", listed)
	}
	if !lists(listed, "keep.env") {
		t.Fatalf("!keep.env did not re-include it:\n%s", listed)
	}
}

func TestTheAlwaysSkippedDirectoriesNeedNoIgnoreFile(t *testing.T) {
	root := t.TempDir()
	seed(t, root, "kept.txt", "x\n")
	for _, dir := range []string{"node_modules", ".git", ".tofu", ".boji"} {
		seed(t, root, dir+"/buried.txt", "x\n")
	}

	listed := globbed(t, root, `{"pattern":"*.txt"}`)
	if !lists(listed, "kept.txt") {
		t.Fatalf("the walk found nothing at all:\n%s", listed)
	}
	if strings.Contains(listed, "buried.txt") {
		t.Fatalf("a directory tofu always skips was walked with no .gitignore present:\n%s", listed)
	}
}

func TestTheOverrideWalksAnIgnoredDirectoryAndSaysTheWalkWasUnfiltered(t *testing.T) {
	root := t.TempDir()
	seed(t, root, ".gitignore", "vendored/\n")
	seed(t, root, "vendored/buried.txt", "x\n")

	filtered := globbed(t, root, `{"pattern":"*.txt"}`)
	if lists(filtered, "vendored/buried.txt") {
		t.Fatalf("the ignored directory was walked by default:\n%s", filtered)
	}

	unfiltered := globbed(t, root, `{"pattern":"*.txt","include_ignored":true}`)
	if !lists(unfiltered, "vendored/buried.txt") {
		t.Fatalf("the override did not reach the ignored directory:\n%s", unfiltered)
	}
	if !strings.Contains(unfiltered, "degraded unfiltered") {
		t.Fatalf("the result does not say the walk was unfiltered:\n%s", unfiltered)
	}
}

func TestADirectoryWithNoIgnoreFileAnywhereWalksEverything(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{"a.txt", "one/b.txt", "one/two/c.txt"} {
		seed(t, root, rel, "x\n")
	}

	listed := globbed(t, root, `{"pattern":"*.txt"}`)
	for _, rel := range []string{"a.txt", "one/b.txt", "one/two/c.txt"} {
		if !lists(listed, rel) {
			t.Fatalf("%s was dropped although no .gitignore exists:\n%s", rel, listed)
		}
	}
	if strings.Contains(listed, "unfiltered") || strings.Contains(listed, "was not applied") {
		t.Fatalf("a plain walk reported an ignore note it had no reason to:\n%s", listed)
	}
}

func TestAnIgnoreFileThatCannotBeUsedDoesNotFailTheWalkAndSaysSo(t *testing.T) {
	unreadable := t.TempDir()
	if err := os.MkdirAll(filepath.Join(unreadable, ".gitignore"), 0o750); err != nil {
		t.Fatalf("seeding a .gitignore that is a directory: %v", err)
	}
	seed(t, unreadable, "kept.txt", "x\n")

	oversize := t.TempDir()
	seed(t, oversize, ".gitignore", strings.Repeat("# padding to pass the cap\n", konst.IgnoreFileBytesCap/8)+"kept.txt\n")
	seed(t, oversize, "kept.txt", "x\n")

	malformed := t.TempDir()
	seed(t, malformed, ".gitignore", "weird\\ name\n")
	seed(t, malformed, "kept.txt", "x\n")

	for _, one := range []struct{ root, says string }{
		{unreadable, "degraded ignore_skipped: an ignore file was not applied"},
		{oversize, "over the"},
		{malformed, "does not support"},
	} {
		listed := globbed(t, one.root, `{"pattern":"*.txt"}`)
		if !strings.Contains(listed, one.says) {
			t.Fatalf("the result does not say the .gitignore was not applied, expected %q:\n%s", one.says, listed)
		}
		if !lists(listed, "kept.txt") {
			t.Fatalf("an unusable .gitignore stopped the walk:\n%s", listed)
		}
	}
}

func TestAProjectInstructionFileIsWalkedEvenWhenAnIgnoreFileExcludesIt(t *testing.T) {
	root := t.TempDir()
	seed(t, root, ".gitignore", "*.md\n")
	seed(t, root, "CLAUDE.md", "the project rules live here\n")
	seed(t, root, "AGENTS.md", "the project rules live here\n")
	seed(t, root, "notes.md", "not instructions\n")

	listed := globbed(t, root, `{"pattern":"*.md"}`)
	read := 0
	for _, line := range strings.Split(turn.ProjectInstructions(root, ""), "\n") {
		from, isInstructions := strings.CutPrefix(line, "instructions from ")
		if !isInstructions {
			continue
		}
		path, _, _ := strings.Cut(from, ",")
		read++
		if name := filepath.Base(path); !lists(listed, name) {
			t.Fatalf("%s is read by the prompt and the .gitignore hid it from glob:\n%s", name, listed)
		}
	}
	if read == 0 {
		t.Fatal("the prompt read no project instruction file at all, so this test proves nothing")
	}
	if lists(listed, "notes.md") {
		t.Fatalf("*.md stopped applying to a file that is not a project instruction file:\n%s", listed)
	}

	grepTool, err := tools.NewGrep(root)
	if err != nil {
		t.Fatalf("building the tool: %v", err)
	}
	found, err := grepTool.Run(context.Background(), json.RawMessage(`{"pattern":"the project rules"}`))
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	if !strings.Contains(found.Content, "CLAUDE.md:1:") {
		t.Fatalf("an agent grepping for its own instructions got nothing:\n%s", found.Content)
	}
}

func TestListPathsSkipsAnIgnoredDirectoryAndKeepsTheInstructionFile(t *testing.T) {
	root := t.TempDir()
	seed(t, root, ".gitignore", ".local/\nCLAUDE.md\n")
	seed(t, root, ".local/planning/board.md", "x\n")
	seed(t, root, "CLAUDE.md", "the project rules live here\n")
	seed(t, root, "internal/turn/loop.go", "x\n")

	listed, err := tools.ListPaths(root)
	if err != nil {
		t.Fatalf("listing the paths under the root: %v", err)
	}
	if slices.Contains(listed, ".local/planning/board.md") {
		t.Errorf("a path under an ignored directory is offered for completion: %v", listed)
	}
	for _, wanted := range []string{"CLAUDE.md", "internal/turn/loop.go"} {
		if !slices.Contains(listed, wanted) {
			t.Errorf("%s is missing from the completion candidates: %v", wanted, listed)
		}
	}
}

func TestEverySupportedPatternConstructMatchesWhatGitWouldMatch(t *testing.T) {
	root := t.TempDir()
	seed(t, root, ".gitignore", strings.Join([]string{
		"star.*",
		"q?.txt",
		"[abc]class.txt",
		"[!abc]class.txt",
		"deep/**/buried.txt",
		"**/anywhere.txt",
		"tail/**",
	}, "\n")+"\n")
	gone := []string{
		"star.txt", "q1.txt", "aclass.txt", "zclass.txt",
		"deep/one/two/buried.txt", "one/anywhere.txt", "tail/one/x.txt",
	}
	kept := []string{"q12.txt", "class.txt", "shallow/buried.txt", "tail.txt"}
	for _, rel := range append(append([]string{}, gone...), kept...) {
		seed(t, root, rel, "x\n")
	}

	listed := globbed(t, root, `{"pattern":"*"}`)
	for _, rel := range gone {
		if lists(listed, rel) {
			t.Fatalf("%s is matched by a supported construct and was still walked:\n%s", rel, listed)
		}
	}
	for _, rel := range kept {
		if !lists(listed, rel) {
			t.Fatalf("%s matches no pattern and was dropped:\n%s", rel, listed)
		}
	}
}
