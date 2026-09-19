package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"boji/internal/turn"
)

const sleeperEnv = "BOJI_BENCH_SLEEPER"

func TestSleeperHelperOutlivesAnyCapTheRunnerSets(t *testing.T) {
	if os.Getenv(sleeperEnv) != "1" {
		t.Skip("helper process only, run by TestExecuteKillsAProcessThatOutlivesTheWallClockCap")
	}
	time.Sleep(time.Minute)
}

func TestExecuteKillsAProcessThatOutlivesTheWallClockCap(t *testing.T) {
	t.Setenv(sleeperEnv, "1")
	plan := Plan{
		Arm:     ArmBoji,
		Command: []string{os.Args[0], "-test.run=TestSleeperHelperOutlivesAnyCapTheRunnerSets", "-test.timeout=0"},
		Caps:    Caps{WallClock: 500 * time.Millisecond},
	}

	execution, err := Execute(context.Background(), plan)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if execution.EndReason != EndReasonWallClock {
		t.Errorf("end reason %q, want %q: an ordinary exit must not stand in for the cap", execution.EndReason, EndReasonWallClock)
	}
	if execution.Elapsed() > 10*time.Second {
		t.Errorf("elapsed %s: the process was not killed at the cap", execution.Elapsed())
	}
}

func TestExecuteReportsAnOrdinaryFailureAsACrashNotTheCap(t *testing.T) {
	plan := Plan{
		Arm:     ArmBoji,
		Command: []string{os.Args[0], "-boji-bench-not-a-flag"},
		Caps:    Caps{WallClock: 30 * time.Second},
	}
	execution, err := Execute(context.Background(), plan)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if execution.EndReason == EndReasonWallClock {
		t.Errorf("a process that exited on its own was recorded as the wall clock cap")
	}
}

var benchedVersions = []int{1, 2}

const (
	liveVersionEnv  = "BOJI_LIVE_VERSION"
	liveArmDirEnv   = "BOJI_LIVE_ARM_DIR"
	transcriptsRoot = ".playground/transcripts"
)

func liveArmPlan(t *testing.T, arm Arm, envName string) (string, Plan) {
	t.Helper()
	if os.Getenv(envName) != "1" {
		t.Skip("live: this runs the " + string(arm) + " arm on the owner's subscription. Set " + envName + "=1 to run it.")
	}
	root := repositoryRoot(t)
	version := 1
	if raw := os.Getenv(liveVersionEnv); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			t.Fatalf("%s=%q is not a version number: %v", liveVersionEnv, raw, err)
		}
		version = parsed
	}
	plan, err := BuildPlan(root, arm, "hono", version)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if dir := os.Getenv(liveArmDirEnv); dir != "" {
		plan.Dir = dir
		if plan.WorkingDir != "" {
			plan.WorkingDir = dir
		}
		for i, arg := range plan.Command {
			if arg == ArmDir(root, arm, "hono") {
				plan.Command[i] = dir
			}
		}
	}
	return root, plan
}

func recordTranscript(t *testing.T, root string, plan Plan, execution Execution) string {
	t.Helper()
	dir := filepath.Join(root, filepath.FromSlash(transcriptsRoot))
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("make the transcript directory: %v", err)
	}
	name := fmt.Sprintf("%s-v%d-%s.txt", plan.Arm, plan.Version, execution.Start.UTC().Format("20060102T150405Z"))
	path := filepath.Join(dir, name)
	body := execution.Stdout
	if execution.Stderr != "" {
		body += "\n=== stderr ===\n" + execution.Stderr
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write the raw transcript: %v", err)
	}
	return path
}

func cliVersion(t *testing.T, plan Plan) string {
	t.Helper()
	out, err := exec.Command(plan.Command[0], "--version").Output()
	if err != nil {
		t.Fatalf("%s --version: %v", plan.Command[0], err)
	}
	return strings.TrimSpace(string(out))
}

func measureArm(execution Execution, src Sources, meta RunMeta) (Row, []string, error) {
	switch meta.Arm {
	case ArmClaude:
		return MeasureClaude(execution, src, meta)
	case ArmCodex:
		return MeasureCodex(execution, src, CodexMeta{
			Arm: meta.Arm, Task: meta.Task, Version: meta.Version, Run: meta.Run,
			CLIVersion: meta.CLIVersion, CredentialKind: meta.CredentialKind, Commit: meta.Commit, Model: codexArmModel,
		})
	case ArmBoji:
		row, gaps := scoreTree(Row{
			Arm: meta.Arm, Task: meta.Task, Version: meta.Version, Run: meta.Run,
			CLIVersion: meta.CLIVersion, CredentialKind: meta.CredentialKind, Commit: meta.Commit,
			Model: bojiArmModel, EndReason: EndReasonDone,
		}, src, []string{"wall clock, turns, tokens and tool calls: this row was scored from the preserved result tree, not from a turn row, because the run that produced the tree wrote none that survived"})
		return row, gaps, nil
	}
	return Row{}, nil, fmt.Errorf("%q is not an arm this bench measures, want claude, codex or boji", meta.Arm)
}

func runLiveArm(t *testing.T, arm Arm, envName string) {
	t.Helper()
	root, plan := liveArmPlan(t, arm, envName)
	t.Logf("running in %s: %s", plan.WorkingDir, Shell(plan.Command))

	execution, err := Execute(context.Background(), plan)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	path := recordTranscript(t, root, plan, execution)
	t.Logf("raw transcript: %s", path)
	if execution.Stderr != "" {
		t.Logf("arm stderr:\n%s", execution.Stderr)
	}

	row, gaps, err := measureArm(execution,
		Sources{ArmDir: plan.Dir, BunBin: "bun", CheckerPath: CheckerPath(root, plan.Version), StartCommit: OwnStartCommit(plan.Dir)},
		RunMeta{Arm: arm, Task: "hono", Version: plan.Version, Run: 1,
			CLIVersion: cliVersion(t, plan), CredentialKind: CredentialKindSubscription, Commit: OwnStartCommit(plan.Dir)})
	if err != nil {
		t.Fatalf("measure %s: %v", path, err)
	}
	t.Logf("\n%s\n%s\n%s", Detail(row, gaps, execution, "n/a, this arm runs no jev gate"), Spend([]Row{row}), Render([]Row{row}))
	t.Logf("row written to %s", writeRecordedRow(t, root, row, gaps))

	if row.Turns == 0 {
		t.Fatalf("the transcript at %s reports no turns, so nothing was measured", path)
	}
}

func TestClaudeArmRunsLiveAndProducesARow(t *testing.T) {
	runLiveArm(t, ArmClaude, "BOJI_LIVE_CLAUDE")
}

func TestCodexArmRunsLiveAndProducesARow(t *testing.T) {
	runLiveArm(t, ArmCodex, "BOJI_LIVE_CODEX")
}

type recordedRow struct {
	Gaps []string `json:"gaps"`
	Row  Row      `json:"row"`
}

const recordedRowsRoot = ".playground/rows"

func writeRecordedRow(t *testing.T, root string, row Row, gaps []string) string {
	t.Helper()
	dir := filepath.Join(root, filepath.FromSlash(recordedRowsRoot))
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("make the row directory: %v", err)
	}
	body, err := json.MarshalIndent(recordedRow{Gaps: gaps, Row: row}, "", "  ")
	if err != nil {
		t.Fatalf("encode the row: %v", err)
	}
	path := filepath.Join(dir, fmt.Sprintf("%s-v%d-run%d.json", row.Arm, row.Version, row.Run))
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("write the row: %v", err)
	}
	return path
}

func TestScoreATreeAnArmAlreadyWrote(t *testing.T) {
	dir := os.Getenv("BOJI_SCORE_DIR")
	if dir == "" {
		t.Skip("scoring: set BOJI_SCORE_DIR to the tree an arm wrote, BOJI_SCORE_ARM to claude, codex or boji, " +
			"BOJI_LIVE_VERSION to the task version, and BOJI_SCORE_TRANSCRIPT to the raw transcript when there is one. " +
			"This spends nothing, it re-reads a run that already happened.")
	}
	root := repositoryRoot(t)
	version, err := strconv.Atoi(os.Getenv(liveVersionEnv))
	if err != nil {
		t.Fatalf("%s: %v", liveVersionEnv, err)
	}
	execution := Execution{Plan: Plan{Caps: Caps{WallClock: defaultWallClockCap, TurnCap: defaultTurnCap}}}
	transcript := os.Getenv("BOJI_SCORE_TRANSCRIPT")
	if transcript != "" {
		stdout, err := os.ReadFile(transcript)
		if err != nil {
			t.Fatalf("read %s: %v", transcript, err)
		}
		written, err := os.Stat(transcript)
		if err != nil {
			t.Fatalf("stat %s: %v", transcript, err)
		}
		execution.Stdout = string(stdout)
		execution.Start, execution.End = written.ModTime(), written.ModTime()
	}

	row, gaps, err := measureArm(execution,
		Sources{ArmDir: dir, BunBin: "bun", CheckerPath: CheckerPath(root, version), StartCommit: OwnStartCommit(dir)},
		RunMeta{Arm: Arm(os.Getenv("BOJI_SCORE_ARM")), Task: "hono", Version: version, Run: 1,
			CLIVersion: os.Getenv("BOJI_SCORE_CLI"), CredentialKind: CredentialKindSubscription, Commit: OwnStartCommit(dir)})
	if err != nil {
		t.Fatalf("measure %s: %v", dir, err)
	}
	t.Logf("\n%s\n%s\n%s", Detail(row, gaps, execution, "n/a, this arm runs no jev gate"), Spend([]Row{row}), Render([]Row{row}))
	t.Logf("row written to %s", writeRecordedRow(t, root, row, gaps))
}

func TestReportEveryArmFromTheRowsOnDisk(t *testing.T) {
	root := repositoryRoot(t)
	paths, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(recordedRowsRoot), "*.json"))
	if err != nil {
		t.Fatalf("glob the recorded rows: %v", err)
	}
	stored := filepath.Join(root, filepath.FromSlash(storedBojiV2Row))
	if _, err := os.Stat(stored); err == nil {
		paths = append(paths, stored)
	}
	if len(paths) == 0 {
		t.Skipf("no recorded rows under %s and no stored boji row at %s", recordedRowsRoot, storedBojiV2Row)
	}
	var rows []Row
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		var recorded recordedRow
		if err := json.Unmarshal(body, &recorded); err != nil {
			t.Fatalf("%s is not a recorded row: %v", path, err)
		}
		rows = append(rows, recorded.Row)
		t.Logf("%s: %s v%d run%d", path, recorded.Row.Arm, recorded.Row.Version, recorded.Row.Run)
	}
	for _, version := range benchedVersions {
		var ofVersion []Row
		for _, r := range rows {
			if r.Version == version {
				ofVersion = append(ofVersion, r)
			}
		}
		if len(ofVersion) == 0 {
			t.Logf("v%d: no arm has a recorded row", version)
			continue
		}
		var unmeasured []string
		for _, r := range ofVersion {
			if r.Turns == 0 || r.WallClockMS == 0 {
				unmeasured = append(unmeasured, string(r.Arm))
			}
		}
		warning := ""
		if len(unmeasured) > 0 {
			warning = "READ FIRST: " + strings.Join(unmeasured, ", ") +
				" carries no turn count and no wall clock, and Render averages that zero in, so every FRONTIER and TURNS line naming it is wrong. Only its GATES and CHECKLIST lines mean anything.\n"
		}
		t.Logf("\nhono v%d, %d arms\n%s%s%s", version, len(ofVersion), warning, Render(ofVersion), Spend(ofVersion))
	}
}

const storedBojiV2Row = "bench/harness/testdata/v2-row/row.json"

func TestBojiArmCommandUsesOnlyFlagsRunParses(t *testing.T) {
	root := repositoryRoot(t)
	bojiBin := buildBoji(t, root)
	plan, err := BuildPlan(root, ArmBoji, "hono", 1)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}

	args := append(append([]string{}, plan.Command[1:]...), "--dry-run")
	cmd := exec.Command(bojiBin, args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("boji %s\nexited %v\n%s", strings.Join(args, " "), err, out)
	}

	bogus := append(append([]string{}, args...), "--wall-clock-cap", "30m")
	cmd = exec.Command(bojiBin, bogus...)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("boji run accepted --wall-clock-cap, so this test cannot tell a real flag from an invented one\n%s", out)
	}
}

func TestEndReasonOfNamesEveryTurnOutcome(t *testing.T) {
	for outcome := turn.OutcomeUnset; outcome <= turn.OutcomeError; outcome++ {
		reason, _ := endReasonOf(outcome)
		if reason == "" {
			t.Errorf("outcome %s produced an empty end reason", outcome)
		}
	}
}

func TestMeasureBojiFillsTheRowFromAStoredTurnRowAndLedger(t *testing.T) {
	root := repositoryRoot(t)
	transcript := filepath.Join(root, harnessTestdataDir)
	session, err := LoadSession(filepath.Join(transcript, "session.json"))
	if err != nil {
		t.Skipf("no stored transcript at %s: the recorded boji run is not in the repository yet and %s is not in this ticket's owns, see the report: %v", transcript, transcript, err)
	}

	meta := RunMeta{Arm: ArmBoji, Task: "hono", Version: 1, Run: 1, CredentialKind: CredentialKindKey}
	src := Sources{
		LedgerDir:   transcript,
		ArmDir:      ArmDir(root, ArmBoji, "hono"),
		BunBin:      "bun",
		CheckerPath: CheckerPath(root, 1),
		StartCommit: OwnStartCommit(ArmDir(root, ArmBoji, "hono")),
	}
	row, gaps := MeasureBoji(session, src, meta)

	if row.Turns != int64(len(session.Steps)) {
		t.Errorf("Turns = %d, want %d from the stored turn row", row.Turns, len(session.Steps))
	}
	wantCredential, _ := credentialOfSpend(session.Spend)
	if row.CredentialKind != wantCredential {
		t.Errorf("CredentialKind = %q, the turn row names its spend %q and that is what decides it", row.CredentialKind, session.Spend)
	}
	if row.CredentialKind == CredentialKindSubscription && row.ModelDollars != nil {
		t.Errorf("a subscription run has no model dollar figure, got %v", *row.ModelDollars)
	}
	if row.CredentialKind == CredentialKindKey && (row.ModelDollars == nil || *row.ModelDollars != session.TotalCostUSD) {
		t.Errorf("ModelDollars did not come from the turn row's own cost %.6f, got %v", session.TotalCostUSD, row.ModelDollars)
	}
	if _, total := ChecklistScore(row); total != gradedChecklistItems {
		t.Errorf("checklist carried %d graded items, want %d", total, gradedChecklistItems)
	}
	for _, gap := range gaps {
		if strings.HasPrefix(gap, "gates:") {
			t.Errorf("the arm directory is its own git repository and the git gates still had no baseline: %s", gap)
		}
	}
}

const (
	harnessTestdataDir   = "bench/harness/testdata/boji-1"
	gradedChecklistItems = 19
)

func TestGradedDropsARecordedItemByItsStatusAndNotByItsNumber(t *testing.T) {
	checks := []ChecklistCheck{
		{Item: 11, Status: ChecklistCheckPassed},
		{Item: 14, Status: ChecklistCheckRecorded},
		{Item: 20, Status: ChecklistCheckFailed},
	}
	kept := graded(checks)
	if len(kept) != 2 {
		t.Fatalf("graded kept %d of %d checks, want 2", len(kept), len(checks))
	}
	if kept[0].Item != 11 || kept[1].Item != 20 {
		t.Fatalf("graded kept items %d and %d, want 11 and 20: v1 numbered its recorded item 11 and v2 does not, so the number cannot decide this", kept[0].Item, kept[1].Item)
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("locate the repository root: %v", err)
	}
	return root
}

func buildBoji(t *testing.T, root string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "boji.exe")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/boji")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("cmd/boji does not build right now, so the flag list cannot be checked against its parser: %v\n%s", err, out)
	}
	return bin
}
