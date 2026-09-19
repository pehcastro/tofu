package task_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"boji/bench/harness"
	"boji/bench/harness/task"
	"boji/internal/sys"
)

var benchedVersions = []int{1, 2}

const (
	storedV2Row   = "bench/harness/testdata/v2-row/row.json"
	v2SessionDir  = "bench/harness/testdata/v2-session"
	v2ArmDir      = ".playground/hono-boji"
	storeV2RowEnv = "BOJI_STORE_V2_ROW"
	v2RowVersion  = 2
)

const gateReasonsAreFirstLineOnly = "first line only: the build gate prints the whole bundle and this file is read by a person"

type StoredRow struct {
	ChecklistRevision string      `json:"checklist_revision"`
	CheckerRevision   string      `json:"checker_revision"`
	PromptRevision    string      `json:"prompt_revision"`
	ArmDir            string      `json:"arm_dir"`
	SessionDir        string      `json:"session_dir"`
	GateReasons       string      `json:"gate_reasons"`
	Gaps              []string    `json:"gaps"`
	Row               harness.Row `json:"row"`
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("locate the repository root: %v", err)
	}
	return root
}

func TestEveryPartOfBothVersionsIsTrackedAndHashable(t *testing.T) {
	root := repositoryRoot(t)
	listed, err := exec.Command("git", "-C", root, "ls-files", "--cached", "--others", "--exclude-standard", task.Dir).Output()
	if err != nil {
		t.Fatalf("git ls-files %s: %v", task.Dir, err)
	}
	tracked := make(map[string]bool)
	for _, line := range strings.Fields(string(listed)) {
		tracked[line] = true
	}

	for _, version := range benchedVersions {
		for _, part := range []task.Part{task.Prompt, task.Checklist, task.Checker} {
			revision, err := task.Revision(root, version, part)
			if err != nil {
				t.Errorf("v%d %s: %v", version, part, err)
				continue
			}
			relative := strings.TrimPrefix(filepath.ToSlash(task.Path("", version, part)), "/")
			if !tracked[relative] {
				t.Errorf("%s hashes to %s and git ignores it, so nobody can prove it was not edited after a result was seen", relative, revision)
				continue
			}
			t.Logf("%s %s", relative, revision)
		}
	}
}

func TestTheHarnessResolvesThePromptAndCheckerFromTheTrackedTask(t *testing.T) {
	root := repositoryRoot(t)
	for _, version := range benchedVersions {
		if got, want := harness.PromptPath(root, version), task.Path(root, version, task.Prompt); got != want {
			t.Errorf("harness.PromptPath(v%d) = %s, want the tracked %s", version, got, want)
		}
		if got, want := harness.CheckerPath(root, version), task.Path(root, version, task.Checker); got != want {
			t.Errorf("harness.CheckerPath(v%d) = %s, want the tracked %s", version, got, want)
		}
	}
}

func TestAMissingCheckerIsAnErrorAndNotASkip(t *testing.T) {
	bun, err := exec.LookPath("bun")
	if err != nil {
		t.Skip("bun is not on PATH, and the checker is a bun script, so this cannot run here")
	}
	missing := task.Path(repositoryRoot(t), 9, task.Checker)
	if _, err := os.Stat(missing); err == nil {
		t.Fatalf("%s exists, so this test cannot tell a missing checker from a present one", missing)
	}

	checks, err := harness.RunChecklist(bun, missing, t.TempDir())
	if err == nil {
		t.Fatalf("RunChecklist returned %d checks and no error for a checker that is not there", len(checks))
	}
	if !strings.Contains(err.Error(), filepath.Base(missing)) {
		t.Errorf("the error does not name the checker it could not find: %v", err)
	}
	t.Logf("missing checker: %v", err)
}

func TestTheStoredV2RowNamesTheChecklistRevisionItScoredAgainst(t *testing.T) {
	root := repositoryRoot(t)
	body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(storedV2Row)))
	if err != nil {
		t.Fatalf("the stored v2 row is the evidence that a run records its checklist revision: %v", err)
	}
	var stored StoredRow
	if err := json.Unmarshal(body, &stored); err != nil {
		t.Fatalf("%s is not a stored row: %v", storedV2Row, err)
	}

	for part, recorded := range map[task.Part]string{
		task.Checklist: stored.ChecklistRevision,
		task.Checker:   stored.CheckerRevision,
		task.Prompt:    stored.PromptRevision,
	} {
		revision, err := task.Revision(root, v2RowVersion, part)
		if err != nil {
			t.Fatal(err)
		}
		if revision != recorded {
			t.Errorf("the stored row scored against %s %s and the tracked file now hashes %s: the row and the file have drifted apart", part, recorded, revision)
		}
	}

	passed, total := harness.ChecklistScore(stored.Row)
	t.Logf("stored v2 row: %s %s v%d run%d, checklist %d/%d against checklist revision %s",
		stored.Row.Arm, stored.Row.Task, stored.Row.Version, stored.Row.Run, passed, total, stored.ChecklistRevision)
	if total == 0 {
		t.Error("the stored row carries no graded checklist item, so it names a revision it did not actually score against")
	}
}

func TestStoreTheV2Row(t *testing.T) {
	if os.Getenv(storeV2RowEnv) != "1" {
		t.Skip("writer only: this runs the gates and the checker over " + v2ArmDir + ". Set " + storeV2RowEnv + "=1 to rewrite " + storedV2Row)
	}
	root := repositoryRoot(t)
	recorded, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(v2SessionDir), "*.json"))
	if err != nil || len(recorded) != 1 {
		t.Fatalf("%s must hold exactly one recorded session and holds %d: %v", v2SessionDir, len(recorded), err)
	}
	session, err := harness.LoadSession(recorded[0])
	if err != nil {
		t.Fatal(err)
	}
	state, err := sys.ProjectStateDir()
	if err != nil {
		t.Fatal(err)
	}

	armDir := filepath.Join(root, filepath.FromSlash(v2ArmDir))
	row, gaps := harness.MeasureBoji(session, harness.Sources{
		LedgerDir:   filepath.Join(state, "log"),
		ArmDir:      armDir,
		BunBin:      "bun",
		CheckerPath: task.Path(root, v2RowVersion, task.Checker),
		StartCommit: harness.OwnStartCommit(armDir),
	}, harness.RunMeta{
		Arm: harness.ArmBoji, Task: "hono", Version: v2RowVersion, Run: 1,
		CLIVersion: sys.Version(), Commit: sys.BuildRevision(),
	})
	for i, gate := range row.Gates {
		line, _, _ := strings.Cut(strings.TrimSpace(gate.Reason), "\n")
		row.Gates[i].Reason = line
	}

	body, err := json.MarshalIndent(StoredRow{
		ChecklistRevision: mustRevise(t, root, task.Checklist),
		CheckerRevision:   mustRevise(t, root, task.Checker),
		PromptRevision:    mustRevise(t, root, task.Prompt),
		ArmDir:            v2ArmDir,
		SessionDir:        v2SessionDir,
		GateReasons:       gateReasonsAreFirstLineOnly,
		Gaps:              gaps,
		Row:               row,
	}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, filepath.FromSlash(storedV2Row))
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, append(body, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %s\n%s", storedV2Row, harness.Render([]harness.Row{row}))
	for _, gap := range gaps {
		t.Logf("gap: %s", gap)
	}
}

func mustRevise(t *testing.T, root string, part task.Part) string {
	t.Helper()
	revision, err := task.Revision(root, v2RowVersion, part)
	if err != nil {
		t.Fatal(err)
	}
	return revision
}
