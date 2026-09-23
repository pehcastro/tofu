package harness

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"tofu/bench/harness/task"
	"tofu/internal/judge/ledger"
)

func playground(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), playgroundRoot)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("make a playground to stage into: %v", err)
	}
	return dir
}

func fakeArm(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "ARM.txt"), []byte("one file, written where an arm would write"), 0o600); err != nil {
		t.Fatalf("the fake arm could not write: %v", err)
	}
}

func filesUnder(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = string(body)
		return nil
	})
	if err != nil {
		t.Fatalf("read the tree at %s: %v", dir, err)
	}
	return out
}

func TestStagingProducesATreeWhoseContentsEqualTheSeedForAllFourTasks(t *testing.T) {
	root := repositoryRoot(t)
	ground := playground(t)
	for _, benched := range task.All() {
		armDir := filepath.Join(ground, benched.Name+"-v"+strconv.Itoa(benched.Version)+"-tofu")
		seed, err := Stage(root, benched.Version, armDir)
		if err != nil {
			t.Fatalf("%s v%d: Stage: %v", benched.Name, benched.Version, err)
		}
		want := filesUnder(t, task.Path(root, benched.Version, task.Seed))
		got := filesUnder(t, armDir)
		if len(want) == 0 {
			t.Fatalf("%s v%d: the committed seed at %s holds no file", benched.Name, benched.Version, seed.Path)
		}
		if len(got) != len(want) {
			t.Errorf("%s v%d: staged %d files, the seed holds %d", benched.Name, benched.Version, len(got), len(want))
		}
		for rel, body := range want {
			if got[rel] != body {
				t.Errorf("%s v%d: %s differs from the seed's copy", benched.Name, benched.Version, rel)
			}
		}
		if seed.Files != len(want) {
			t.Errorf("%s v%d: the record says %d files, the seed holds %d", benched.Name, benched.Version, seed.Files, len(want))
		}
		t.Logf("%s v%d: %s, %d files, %s", benched.Name, benched.Version, seed.Path, seed.Files, seed.Digest)
	}
}

func TestTwoRepeatsOfTheSameTaskStartFromIdenticalBytes(t *testing.T) {
	root := repositoryRoot(t)
	armDir := filepath.Join(playground(t), "notes-v3-tofu")

	first, err := Stage(root, 3, armDir)
	if err != nil {
		t.Fatalf("stage repeat 1: %v", err)
	}
	before := filesUnder(t, armDir)

	fakeArm(t, armDir)
	if err := os.WriteFile(filepath.Join(armDir, "src", "app.ts"), []byte("the arm rewrote this"), 0o600); err != nil {
		t.Fatalf("dirty a seed file: %v", err)
	}
	if err := os.Remove(filepath.Join(armDir, "README.md")); err != nil {
		t.Fatalf("delete a seed file: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(armDir, "node_modules", "hono"), 0o750); err != nil {
		t.Fatalf("leave an install behind: %v", err)
	}

	second, err := Stage(root, 3, armDir)
	if err != nil {
		t.Fatalf("stage repeat 2 over the dirtied tree: %v", err)
	}
	if first.Digest != second.Digest {
		t.Fatalf("repeat 1 started from %s and repeat 2 from %s", first.Digest, second.Digest)
	}
	after := filesUnder(t, armDir)
	if len(after) != len(before) {
		t.Fatalf("repeat 2 starts from %d files and repeat 1 from %d", len(after), len(before))
	}
	for rel, body := range before {
		if after[rel] != body {
			t.Errorf("%s survived the restage with different bytes", rel)
		}
	}
	if _, err := os.Stat(filepath.Join(armDir, "node_modules")); !os.IsNotExist(err) {
		t.Errorf("node_modules survived the restage, so repeat 2 starts from an install repeat 1 made: %v", err)
	}
}

func TestEveryBenchedTaskGetsItsOwnArmDirectory(t *testing.T) {
	root := repositoryRoot(t)
	seen := map[string]string{}
	for _, benched := range task.All() {
		for _, arm := range []Arm{ArmClaude, ArmCodex, ArmTofu} {
			dir := ArmDir(root, arm, benched.Name, benched.Version)
			label := benched.Name + " v" + strconv.Itoa(benched.Version) + " " + string(arm)
			if held, taken := seen[dir]; taken {
				t.Errorf("%s and %s are both pointed at %s", label, held, dir)
			}
			seen[dir] = label
			if !strings.Contains(filepath.Base(dir), "v"+strconv.Itoa(benched.Version)) {
				t.Errorf("%s: the directory name %s does not say which version it is", label, filepath.Base(dir))
			}
		}
	}
	if ArmDir(root, ArmClaude, "hono", 2) == ArmDir(root, ArmClaude, "notes", 3) {
		t.Error("v3 reuses the directory v2 left behind")
	}
}

func TestARowRecordsWhichSeedItStartedFrom(t *testing.T) {
	root := repositoryRoot(t)
	seed, err := SeedOf(root, 3)
	if err != nil {
		t.Fatalf("SeedOf: %v", err)
	}
	plan := Plan{Arm: ArmTofu, Task: "notes", Version: 3, Repeats: 2, Seed: seed}
	for _, meta := range plan.Runs() {
		if meta.Seed.Digest != seed.Digest {
			t.Fatalf("run %d carries seed %q, want %q", meta.Run, meta.Seed.Digest, seed.Digest)
		}
		meta.CredentialKind = CredentialKindSubscription
		row, _, err := ParseTofu(t.TempDir(), ledger.Filter{}, meta)
		if err != nil {
			t.Fatalf("ParseTofu: %v", err)
		}
		body, err := json.Marshal(row)
		if err != nil {
			t.Fatalf("marshal the row: %v", err)
		}
		if !strings.Contains(string(body), seed.Digest) || !strings.Contains(string(body), "v3.seed") {
			t.Fatalf("the row does not say which seed it started from: %s", body)
		}
	}
}

func TestATreeThatDoesNotMatchItsSeedIsRefusedBeforeAnArmRuns(t *testing.T) {
	root := repositoryRoot(t)
	armDir := filepath.Join(playground(t), "notes-v3-tofu")
	seed, err := Stage(root, 3, armDir)
	if err != nil {
		t.Fatalf("Stage: %v", err)
	}
	if err := seed.Check(armDir); err != nil {
		t.Fatalf("a freshly staged tree was refused: %v", err)
	}
	fakeArm(t, armDir)
	err = seed.Check(armDir)
	if err == nil {
		t.Fatal("a tree the fake arm had already written into was accepted as the seed")
	}
	if !strings.Contains(err.Error(), seed.Digest) {
		t.Errorf("the refusal does not name the seed it wanted: %v", err)
	}
	t.Logf("refused: %v", err)
}

func TestStagingRefusesAPathThatIsNotInsideThePlayground(t *testing.T) {
	root := repositoryRoot(t)
	outside := t.TempDir()
	keep := filepath.Join(outside, "a-person-put-this-here.txt")
	if err := os.WriteFile(keep, []byte("not the harness's to delete"), 0o600); err != nil {
		t.Fatalf("write the file that must survive: %v", err)
	}
	for _, dir := range []string{outside, filepath.Join(outside, "notes-v3-tofu"), filepath.Join(outside, ".playground")} {
		if _, err := Stage(root, 3, dir); err == nil {
			t.Fatalf("Stage accepted %s, which is not a child of a playground", dir)
		}
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("the refused staging deleted something anyway: %v", err)
	}
}

func TestTheWallClockCostOfStagingEachSeed(t *testing.T) {
	root := repositoryRoot(t)
	ground := playground(t)
	for _, benched := range task.All() {
		armDir := filepath.Join(ground, benched.Name+"-v"+strconv.Itoa(benched.Version)+"-timed")
		start := time.Now()
		seed, err := Stage(root, benched.Version, armDir)
		if err != nil {
			t.Fatalf("%s v%d: Stage: %v", benched.Name, benched.Version, err)
		}
		t.Logf("%s v%d: %d files staged in %s, and a full run pays it once per repeat",
			benched.Name, benched.Version, seed.Files, time.Since(start).Round(time.Microsecond))
	}
}
