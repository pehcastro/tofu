package task_test

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"tofu/bench/harness"
	"tofu/bench/harness/task"
)

const (
	v3SeedDir    = "bench/harness/task/v3.seed"
	v3SolvedDir  = "bench/harness/task/testdata/v3-solved"
	v4SeedDir    = "bench/harness/task/v4.seed"
	v4AnsweredIn = "bench/harness/task/testdata/v4-answered"
	v4DecoyedIn  = "bench/harness/task/testdata/v4-decoyed"
)

func bunOrSkip(t *testing.T) string {
	t.Helper()
	bun, err := exec.LookPath("bun")
	if err != nil {
		t.Skip("skipped, and counted as a skip: bun is not on PATH, and every checker in this directory is a bun script")
	}
	return bun
}

func copyTree(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.WalkDir(from, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		target := filepath.Join(to, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o750)
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return err
		}
		return os.WriteFile(target, body, 0o600)
	})
	if err != nil {
		t.Fatalf("copying %s to %s: %v", from, to, err)
	}
}

func treeFrom(t *testing.T, root string, parts ...string) string {
	t.Helper()
	arm := t.TempDir()
	for _, part := range parts {
		copyTree(t, filepath.Join(root, filepath.FromSlash(part)), arm)
	}
	return arm
}

func scored(t *testing.T, bun, checker, armDir, label string) map[int]harness.ChecklistCheck {
	t.Helper()
	checks, err := harness.RunChecklist(bun, checker, armDir)
	if err != nil {
		t.Fatalf("%s: %v", label, err)
	}
	if len(checks) == 0 {
		t.Fatalf("%s: the checker graded nothing", label)
	}
	byItem := make(map[int]harness.ChecklistCheck, len(checks))
	for _, check := range checks {
		byItem[check.Item] = check
		t.Logf("%s item %d %s %s", label, check.Item, check.Status, check.Reason)
	}
	return byItem
}

func mustFail(t *testing.T, scoredBy map[int]harness.ChecklistCheck, label string, items ...int) {
	t.Helper()
	for _, item := range items {
		check, held := scoredBy[item]
		if !held {
			t.Errorf("%s: item %d was never graded", label, item)
			continue
		}
		if check.Status == harness.ChecklistCheckPassed {
			t.Errorf("%s: item %d passed against a tree that was built to fail it, so the item checks nothing", label, item)
		}
	}
}

func mustAllPass(t *testing.T, scoredBy map[int]harness.ChecklistCheck, label string, count int) {
	t.Helper()
	for item := 1; item <= count; item++ {
		check, held := scoredBy[item]
		if !held {
			t.Errorf("%s: item %d was never graded", label, item)
			continue
		}
		if check.Status != harness.ChecklistCheckPassed {
			t.Errorf("%s: item %d is %s: %s", label, item, check.Status, check.Reason)
		}
	}
	if len(scoredBy) != count {
		t.Errorf("%s: graded %d items, want %d", label, len(scoredBy), count)
	}
}

const v3ItemCount = 22

func TestTheV3CheckerRefusesTheUntouchedSeedAndPassesTheSolvedTree(t *testing.T) {
	bun := bunOrSkip(t)
	root := repositoryRoot(t)
	checker := task.Path(root, 3, task.Checker)

	floor := scored(t, bun, checker, treeFrom(t, root, v3SeedDir), "v3 untouched seed")
	mustFail(t, floor, "v3 untouched seed", 5, 6, 7, 9, 10, 11, 13, 14, 15, 17, 18, 19, 20, 21)

	solved := scored(t, bun, checker, treeFrom(t, root, v3SeedDir, v3SolvedDir), "v3 solved")
	mustAllPass(t, solved, "v3 solved", v3ItemCount)
}

const v4ItemCount = 9

func TestTheV4CheckerRefusesTheDecoyAnswerAndPassesTheAnsweredTree(t *testing.T) {
	bun := bunOrSkip(t)
	root := repositoryRoot(t)
	checker := task.Path(root, 4, task.Checker)

	decoyed := treeFrom(t, root, v4SeedDir, v4DecoyedIn)
	edited := filepath.Join(decoyed, filepath.FromSlash("src/plan/overrides.ts"))
	body, err := os.ReadFile(edited)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(edited, append(body, []byte("export const stray = 1\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(decoyed, "NOTES.md"), []byte("working notes\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	wrong := scored(t, bun, checker, decoyed, "v4 decoy answer in an edited tree")
	mustFail(t, wrong, "v4 decoy answer in an edited tree", 3, 4, 5, 6, 7, 8, 9)

	answered := scored(t, bun, checker, treeFrom(t, root, v4SeedDir, v4AnsweredIn), "v4 answered")
	mustAllPass(t, answered, "v4 answered", v4ItemCount)
}

func TestNeitherNewPromptNamesItsOwnAnswer(t *testing.T) {
	root := repositoryRoot(t)
	leaks := map[int][]string{
		3: {"hasExpired", "hidden, not deleted"},
		4: {"plan.capability.withheld", "withheldByPlan", "overrides.ts", "ndjson"},
	}
	for version, needles := range leaks {
		body, err := os.ReadFile(task.Path(root, version, task.Prompt))
		if err != nil {
			t.Fatal(err)
		}
		prompt := string(body)
		for _, needle := range needles {
			if strings.Contains(prompt, needle) {
				t.Errorf("the v%d prompt contains %q, which is part of its own answer, so it cannot separate an arm that read the tree from one that read the prompt", version, needle)
			}
		}
	}
}

func TestBothSeedTreesAreCommittedAndTheirSizeIsStated(t *testing.T) {
	root := repositoryRoot(t)
	listed, err := exec.Command("git", "-C", root, "ls-files", "--cached", "--others", "--exclude-standard", v3SeedDir, v4SeedDir).Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	tracked := make(map[string]bool)
	for _, line := range strings.Fields(string(listed)) {
		tracked[line] = true
	}

	for _, seed := range []string{v3SeedDir, v4SeedDir} {
		files, lines := 0, 0
		err := filepath.WalkDir(filepath.Join(root, filepath.FromSlash(seed)), func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			slashed := filepath.ToSlash(relative)
			if !tracked[slashed] {
				t.Errorf("%s is in the seed tree and git ignores it, so no arm would start from the tree this repository thinks it committed", slashed)
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			files++
			lines += strings.Count(string(body), "\n")
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if files == 0 {
			t.Fatalf("%s holds no file", seed)
		}
		t.Logf("%s: %d files, %d lines", seed, files, lines)
	}
}
