package harness

import (
	"reflect"
	"regexp"
	"testing"
)

var testFuncPattern = regexp.MustCompile(`func Test\w+\(`)

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
	task := taskFor("testdata/gates/repo_pass",
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
		dir      string
		build    []string
		lint     []string
		test     []string
		checklst []string
		gate     string
	}{
		{"build", "testdata/gates/repo_fail_build", fail, ok, ok, ok, "build"},
		{"lint", "testdata/gates/repo_fail_lint", ok, fail, ok, ok, "lint"},
		{"test", "testdata/gates/repo_fail_test", ok, ok, fail, ok, "test"},
		{"checklist", "testdata/gates/repo_fail_checklist", ok, ok, ok, fail, "checklist: item a"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			task := taskFor(c.dir, c.build, c.lint, c.test, c.checklst)
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
	task := taskFor("testdata/gates/repo_deleted_tests",
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
	outcome := runCommandGate("build", "testdata/gates/repo_missing_command", []string{"boji-bench-gate-missing-binary-xyz"})
	if outcome.Status == GateStatusPassed {
		t.Fatalf("missing command reported passed, must never be passed")
	}
	if outcome.Status != GateStatusCouldNotEvaluate {
		t.Fatalf("got status %q, want could_not_evaluate", outcome.Status)
	}
}

func TestCommandGate_NoCommandDeclaredIsCouldNotEvaluate(t *testing.T) {
	outcome := runCommandGate("checklist: undeclared", "testdata/gates/repo_pass", nil)
	if outcome.Status != GateStatusCouldNotEvaluate {
		t.Fatalf("got status %q, want could_not_evaluate", outcome.Status)
	}
}

func runForArm(armLabel string, task Task) []GateOutcome {
	_ = armLabel
	return RunGates(task)
}

func TestNoGateReadsArmName(t *testing.T) {
	task := taskFor("testdata/gates/repo_pass",
		[]string{"go", "run", "ok.go"},
		[]string{"go", "run", "ok.go"},
		[]string{"go", "run", "ok.go"},
		[]string{"go", "run", "ok.go"})

	claude := runForArm("claude", task)
	codex := runForArm("codex", task)
	boji := runForArm("boji", task)

	if !reflect.DeepEqual(claude, codex) || !reflect.DeepEqual(codex, boji) {
		t.Fatalf("results differ by arm label:\nclaude=%+v\ncodex=%+v\nboji=%+v", claude, codex, boji)
	}
}

func TestDiffCostGate(t *testing.T) {
	outcome, cost := RunDiffCostGate("testdata/gates/repo_pass", "start")
	if outcome.Status != GateStatusPassed {
		t.Fatalf("got status %q, reason %q", outcome.Status, outcome.Reason)
	}
	want := DiffCost{Files: 2, Additions: 4, Deletions: 1}
	if cost != want {
		t.Fatalf("got %+v, want %+v", cost, want)
	}
}

func TestDiffCostGate_BadStartCommitIsCouldNotEvaluate(t *testing.T) {
	outcome, _ := RunDiffCostGate("testdata/gates/repo_pass", "not-a-real-commit")
	if outcome.Status != GateStatusCouldNotEvaluate {
		t.Fatalf("got status %q, want could_not_evaluate", outcome.Status)
	}
}

func TestCountTestsInTree(t *testing.T) {
	count, err := countTestsInTree("testdata/gates/repo_deleted_tests", "tests", testFuncPattern)
	if err != nil {
		t.Fatalf("countTestsInTree: %v", err)
	}
	if count != 2 {
		t.Fatalf("got %d, want 2", count)
	}
}

func TestCountTestsAtCommit(t *testing.T) {
	count, err := countTestsAtCommit("testdata/gates/repo_deleted_tests", "start", "tests", testFuncPattern)
	if err != nil {
		t.Fatalf("countTestsAtCommit: %v", err)
	}
	if count != 3 {
		t.Fatalf("got %d, want 3", count)
	}
}
