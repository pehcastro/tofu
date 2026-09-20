package harness

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"testing"
)

var testFuncPattern = regexp.MustCompile(`func Test\w+\(`)

const okGo = `package main

import "fmt"

func main() {
	fmt.Println("ok")
}
`

const failGo = `package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "boom")
	os.Exit(1)
}
`

const testAlpha = `package tests

func TestAlpha(t *T) {}
`

const testBeta = `package tests

func TestBeta(t *T) {}
`

const testGamma = `package tests

func TestGamma(t *T) {}
`

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=bench fixture", "GIT_AUTHOR_EMAIL=bench@tofu.local",
		"GIT_COMMITTER_NAME=bench fixture", "GIT_COMMITTER_EMAIL=bench@tofu.local")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for path, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", full, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}
}

func newRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	writeFiles(t, dir, files)
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", "start")
	runGit(t, dir, "tag", "start")
	return dir
}

func baseFiles() map[string]string {
	return map[string]string{
		"ok.go":      okGo,
		"fail.go":    failGo,
		"tests/a.go": testAlpha,
	}
}

func newRepoPass(t *testing.T) string {
	t.Helper()
	dir := newRepo(t, map[string]string{
		"ok.go":      okGo,
		"fail.go":    failGo,
		"tests/a.go": testAlpha,
		"tests/b.go": testBeta,
		"notes.txt":  "a\nb\nc\n",
	})
	writeFiles(t, dir, map[string]string{
		"notes.txt": "a\nx\nc\nd\n",
		"extra.txt": "e\nf\n",
	})
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", "second")
	return dir
}

func newRepoDeletedTests(t *testing.T) string {
	t.Helper()
	dir := newRepo(t, map[string]string{
		"ok.go":      okGo,
		"fail.go":    failGo,
		"tests/a.go": testAlpha,
		"tests/b.go": testBeta,
		"tests/c.go": testGamma,
	})
	if err := os.Remove(filepath.Join(dir, "tests", "c.go")); err != nil {
		t.Fatalf("remove tests/c.go: %v", err)
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", "delete test c")
	return dir
}

func findGate(t *testing.T, outcomes []GateOutcome, name string) GateOutcome {
	t.Helper()
	for _, o := range outcomes {
		if o.Name == name {
			return o
		}
	}
	t.Fatalf("gate %q not found in %v", name, outcomes)
	return GateOutcome{}
}

func taskFor(dir string, build, lint, test, checklist []string) Task {
	return Task{
		Dir:         dir,
		StartCommit: "start",
		Build:       build,
		Lint:        lint,
		Test:        TestGateCommand{Command: test, TestDirRel: "tests", Pattern: testFuncPattern},
		Checklist:   []ChecklistItem{{Item: "item a", Command: checklist}},
	}
}

func TestRunGates_AllPass(t *testing.T) {
	task := taskFor(newRepoPass(t),
		[]string{"go", "run", "ok.go"},
		[]string{"go", "run", "ok.go"},
		[]string{"go", "run", "ok.go"},
		[]string{"go", "run", "ok.go"})
	outcomes := RunGates(task)
	for _, o := range outcomes {
		if o.Status != GateStatusPassed {
			t.Errorf("gate %q: got status %q, reason %q, want passed", o.Name, o.Status, o.Reason)
		}
	}
	t.Logf("outcomes: %+v", outcomes)
}

func TestRunGates_EachGateFailsInTurn(t *testing.T) {
	ok := []string{"go", "run", "ok.go"}
	fail := []string{"go", "run", "fail.go"}

	cases := []struct {
		name     string
		build    []string
		lint     []string
		test     []string
		checklst []string
		gate     string
	}{
		{"build", fail, ok, ok, ok, "build"},
		{"lint", ok, fail, ok, ok, "lint"},
		{"test", ok, ok, fail, ok, "test"},
		{"checklist", ok, ok, ok, fail, "checklist: item a"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			task := taskFor(newRepo(t, baseFiles()), c.build, c.lint, c.test, c.checklst)
			outcomes := RunGates(task)
			gate := findGate(t, outcomes, c.gate)
			if gate.Status != GateStatusFailed {
				t.Fatalf("gate %q: got status %q, want failed", c.gate, gate.Status)
			}
			if gate.Reason == "" {
				t.Fatalf("gate %q failed with no reason", c.gate)
			}
			t.Logf("gate %q failed with reason: %q", c.gate, gate.Reason)
		})
	}
}

func TestTestGate_DeletedTestsFailsWithCounts(t *testing.T) {
	task := taskFor(newRepoDeletedTests(t),
		[]string{"go", "run", "ok.go"},
		[]string{"go", "run", "ok.go"},
		[]string{"go", "run", "ok.go"},
		[]string{"go", "run", "ok.go"})
	outcomes := RunGates(task)
	gate := findGate(t, outcomes, "test")
	if gate.Status != GateStatusFailed {
		t.Fatalf("got status %q, want failed", gate.Status)
	}
	want := "tests deleted: 3 before, 2 after"
	if gate.Reason != want {
		t.Fatalf("got reason %q, want %q", gate.Reason, want)
	}
	t.Logf("test gate reason: %q", gate.Reason)
}

func TestCommandGate_MissingCommandIsCouldNotEvaluate(t *testing.T) {
	outcome := runCommandGate("build", t.TempDir(), []string{"tofu-bench-gate-missing-binary-xyz"})
	if outcome.Status == GateStatusPassed {
		t.Fatalf("missing command reported passed, must never be passed")
	}
	if outcome.Status != GateStatusCouldNotEvaluate {
		t.Fatalf("got status %q, want could_not_evaluate", outcome.Status)
	}
}

func TestCommandGate_NoCommandDeclaredIsCouldNotEvaluate(t *testing.T) {
	outcome := runCommandGate("checklist: undeclared", t.TempDir(), nil)
	if outcome.Status != GateStatusCouldNotEvaluate {
		t.Fatalf("got status %q, want could_not_evaluate", outcome.Status)
	}
}

func runForArm(armLabel string, task Task) []GateOutcome {
	_ = armLabel
	return RunGates(task)
}

func TestNoGateReadsArmName(t *testing.T) {
	task := taskFor(newRepo(t, baseFiles()),
		[]string{"go", "run", "ok.go"},
		[]string{"go", "run", "ok.go"},
		[]string{"go", "run", "ok.go"},
		[]string{"go", "run", "ok.go"})

	claude := runForArm("claude", task)
	codex := runForArm("codex", task)
	tofu := runForArm("tofu", task)

	if !reflect.DeepEqual(claude, codex) || !reflect.DeepEqual(codex, tofu) {
		t.Fatalf("results differ by arm label:\nclaude=%+v\ncodex=%+v\ntofu=%+v", claude, codex, tofu)
	}
}

func TestDiffCostGate(t *testing.T) {
	outcome, cost := RunDiffCostGate(newRepoPass(t), "start")
	if outcome.Status != GateStatusPassed {
		t.Fatalf("got status %q, reason %q", outcome.Status, outcome.Reason)
	}
	want := DiffCost{Files: 2, Additions: 4, Deletions: 1}
	if cost != want {
		t.Fatalf("got %+v, want %+v", cost, want)
	}
}

func TestDiffCostGateCountsFilesTheArmAddedWithoutCommittingThem(t *testing.T) {
	dir := newRepo(t, baseFiles())
	writeFiles(t, dir, map[string]string{
		"ok.go":       okGo + "\n",
		"brandnew.go": "package main\n\nfunc added() {}\n",
	})

	outcome, cost := RunDiffCostGate(dir, "start")
	if outcome.Status != GateStatusPassed {
		t.Fatalf("got status %q, reason %q", outcome.Status, outcome.Reason)
	}
	if cost.Files != 2 {
		t.Errorf("Files = %d, want 2: an arm that never runs git commit still wrote brandnew.go, and a gate that only reads git diff prices its work at nothing", cost.Files)
	}
	if cost.Additions != 4 {
		t.Errorf("Additions = %d, want 4: one blank line in ok.go and three in brandnew.go", cost.Additions)
	}
}

func TestDiffCostGate_BadStartCommitIsCouldNotEvaluate(t *testing.T) {
	outcome, _ := RunDiffCostGate(newRepo(t, baseFiles()), "not-a-real-commit")
	if outcome.Status != GateStatusCouldNotEvaluate {
		t.Fatalf("got status %q, want could_not_evaluate", outcome.Status)
	}
}

func TestCountTestsInTree(t *testing.T) {
	count, err := countTestsInTree(newRepoDeletedTests(t), "tests", testFuncPattern)
	if err != nil {
		t.Fatalf("countTestsInTree: %v", err)
	}
	if count != 2 {
		t.Fatalf("got %d, want 2", count)
	}
}

func TestCountTestsAtCommit(t *testing.T) {
	count, err := countTestsAtCommit(newRepoDeletedTests(t), "start", "tests", testFuncPattern)
	if err != nil {
		t.Fatalf("countTestsAtCommit: %v", err)
	}
	if count != 3 {
		t.Fatalf("got %d, want 3", count)
	}
}
