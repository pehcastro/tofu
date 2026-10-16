package guard

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "core.autocrlf=false"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
}

func put(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commitAll(t *testing.T, repo string) {
	t.Helper()
	gitRun(t, repo, "add", "-A")
	gitRun(t, repo, "commit", "-q", "--allow-empty", "-m", "work")
}

func stampRepo(t *testing.T) string {
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q")
	put(t, repo, ".gitignore", ".local/\n")
	put(t, repo, "internal/turn/turn.go", "package turn\n")
	put(t, repo, "internal/konst/konst.go", "package konst\n")
	put(t, repo, ".local/boji/tickets/review/TOFU-1.md", "---\nid: TOFU-1\nowns:\n  - internal/turn/**\n---\n")
	put(t, repo, ".local/boji/tickets/review/TOFU-2.md", "---\nid: TOFU-2\nowns:\n  - internal/guard/**\n  - docs/**\n---\n")
	put(t, repo, ".local/boji/tickets/review/TOFU-3.md", "---\nid: TOFU-3\nowns:\n  - internal/**\n---\n")
	put(t, repo, ".local/boji/tickets/review/TOFU-4.md", "---\nid: TOFU-4\nowns:\n  - cmd/tofu/guard.go\n---\n")
	commitAll(t, repo)
	return repo
}

func stampHead(t *testing.T, repo, kind string) {
	t.Helper()
	tree, _, err := HeadTree(repo)
	if err != nil {
		t.Fatal(err)
	}
	put(t, StampDir(repo), kind+"/"+tree, "tree "+tree+"\n")
}

func shellHook(t *testing.T, repo, command string) string {
	t.Helper()
	stdin, _ := json.Marshal(map[string]any{"tool_name": "Bash", "tool_input": map[string]string{"command": command}})
	answer, err := Hook(Shell, Env{Root: repo, Temp: t.TempDir()}, stdin)
	if err != nil {
		t.Fatal(err)
	}
	return answer.Stdout
}

func TestDoneMoveNeedsAFlowsStampForHeadAndCommittedWork(t *testing.T) {
	t.Setenv("TOFU_STAMP_DIR", "")
	repo := stampRepo(t)
	const otherTree = "0123456789abcdef0123456789abcdef01234567"
	move := "tofu boardy move TOFU-1 done"
	steps := []struct {
		name, command, want string
		before              func()
		refused             bool
	}{
		{name: "a turn ticket with no stamp", command: move + " --reason ok", refused: true, want: "go run ./bench/flows"},
		{name: "a broad internal glob touches turn", command: "tofu boardy move TOFU-3 done", refused: true},
		{name: "one file in cmd/tofu", command: "tofu boardy move TOFU-4 done", refused: true},
		{name: "flags before the id", command: `tofu boardy move --reason "all good" --as orchestrator TOFU-1 done`, refused: true},
		{name: "chained after another command", command: `cd x && tofu.exe boardy move TOFU-1 done`, refused: true},
		{name: "a ticket outside the gated areas", command: "tofu boardy move TOFU-2 done"},
		{name: "a move to review", command: "tofu boardy move TOFU-1 review"},
		{name: "a log line quoting the move", command: `tofu boardy log TOFU-2 "next: tofu boardy move TOFU-1 done"`},
		{name: "an unknown ticket", command: "tofu boardy move TOFU-99 done"},
		{name: "a stamp for another tree", before: func() { put(t, repo, ".local/flows/"+otherTree, "tree "+otherTree+"\n") }, command: move, refused: true},
		{name: "a treewide stamp is not a flows stamp", before: func() { stampHead(t, repo, "treewide") }, command: move, refused: true},
		{name: "a stamp for HEAD", before: func() { stampHead(t, repo, "flows") }, command: move},
		{name: "another ticket's uncommitted Go", before: func() { put(t, repo, "internal/guard/guard.go", "package guard\n") }, command: move},
		{name: "an uncommitted edit in an owned path", before: func() { put(t, repo, "internal/turn/turn.go", "package turn\n\nconst X = 1\n") }, command: move, refused: true, want: "internal/turn/turn.go"},
		{name: "committed but not stamped", before: func() { commitAll(t, repo) }, command: move, refused: true, want: "go run ./bench/flows"},
		{name: "stamped after the commit", before: func() { stampHead(t, repo, "flows") }, command: move},
		{name: "an untracked file in an owned path", before: func() { put(t, repo, "internal/turn/new.go", "package turn\n") }, command: move, refused: true, want: "internal/turn/new.go"},
	}
	for _, step := range steps {
		if step.before != nil {
			step.before()
		}
		out := shellHook(t, repo, step.command)
		refused := strings.Contains(out, `"deny"`)
		if refused != step.refused {
			t.Fatalf("%s: refused %v, want %v: %s", step.name, refused, step.refused, out)
		}
		if !strings.Contains(out, step.want) {
			t.Fatalf("%s: %q missing from %s", step.name, step.want, out)
		}
	}
}

func TestStampDirComesFromTheEnvironment(t *testing.T) {
	repo := stampRepo(t)
	elsewhere := t.TempDir()
	t.Setenv("TOFU_STAMP_DIR", elsewhere)
	stampHead(t, repo, "flows")
	if _, err := os.Stat(filepath.Join(repo, ".local", "flows")); err == nil {
		t.Fatal("the stamp went to the repository, not TOFU_STAMP_DIR")
	}
	if out := shellHook(t, repo, "tofu boardy move TOFU-1 done"); out != "" {
		t.Fatalf("a stamp under TOFU_STAMP_DIR was not read: %s", out)
	}
}

func TestDoneMoveOutsideAGitRepositoryIsRefused(t *testing.T) {
	root := t.TempDir()
	put(t, root, ".local/boji/tickets/review/TOFU-1.md", "---\nid: TOFU-1\nowns:\n  - internal/turn/**\n---\n")
	if out := shellHook(t, root, "tofu boardy move TOFU-1 done"); !strings.Contains(out, `"deny"`) {
		t.Fatalf("a tree that cannot be read was allowed: %q", out)
	}
}

func TestHeadTreeWithNoGoFiles(t *testing.T) {
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q")
	put(t, repo, "README.md", "x\n")
	commitAll(t, repo)
	if tree, dirty, err := HeadTree(repo); err != nil || len(tree) != 40 || len(dirty) != 0 {
		t.Fatalf("tree %q, dirty %v, err %v", tree, dirty, err)
	}
}

func TestReleaseCommitNeedsBothStampsOnHead(t *testing.T) {
	t.Setenv("TOFU_STAMP_DIR", "")
	repo := stampRepo(t)
	dir := t.TempDir()
	message := filepath.Join(dir, "COMMIT_EDITMSG")
	put(t, dir, "COMMIT_EDITMSG", "chore(release): 9.9.9\n")
	steps := []struct {
		name       string
		before     func()
		want, lack []string
	}{
		{name: "no stamps", want: []string{"go run ./bench/flows", "go run ./bench/treewide"}},
		{name: "flows only", before: func() { stampHead(t, repo, "flows") }, want: []string{"go run ./bench/treewide"}, lack: []string{"go run ./bench/flows"}},
		{name: "both, and the release files staged", before: func() {
			stampHead(t, repo, "treewide")
			put(t, repo, "internal/konst/konst.go", "package konst\n\nconst Version = \"9.9.9\"\n")
			put(t, repo, "CHANGELOG.md", "# Changelog\n")
			put(t, repo, "docs/x.md", "x\n")
			put(t, repo, "internal/turn/turn.go", "package turn\n\nconst Unstaged = 1\n")
			gitRun(t, repo, "add", "internal/konst", "CHANGELOG.md", "docs")
		}, want: []string{"CHANGELOG.md has no entry"}, lack: []string{"bench/", "staged"}},
		{name: "a staged Go change outside the release files", before: func() { gitRun(t, repo, "add", "internal/turn/turn.go") }, want: []string{"internal/turn/turn.go"}},
	}
	for _, step := range steps {
		if step.before != nil {
			step.before()
		}
		answer := CommitMsg(repo, message)
		if answer.Code == 0 {
			t.Fatalf("%s: the release commit was allowed", step.name)
		}
		for _, text := range step.want {
			if !strings.Contains(answer.Stderr, text) {
				t.Fatalf("%s: %q missing from %s", step.name, text, answer.Stderr)
			}
		}
		for _, text := range step.lack {
			if strings.Contains(answer.Stderr, text) {
				t.Fatalf("%s: %q present in %s", step.name, text, answer.Stderr)
			}
		}
	}
	put(t, dir, "COMMIT_EDITMSG", "fix(turn): a plain change\n")
	if answer := CommitMsg(repo, message); answer.Code != 0 {
		t.Fatalf("a non-release commit was gated: %s", answer.Stderr)
	}
}
