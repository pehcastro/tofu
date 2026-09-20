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

	"tofu/internal/turn"
)

const sleeperEnv = "TOFU_BENCH_SLEEPER"

func TestSleeperHelperOutlivesAnyCapTheRunnerSets(t *testing.T) {
	if os.Getenv(sleeperEnv) != "1" {
		t.Skip("helper process only, run by TestExecuteKillsAProcessThatOutlivesTheWallClockCap")
	}
	time.Sleep(time.Minute)
}

func TestExecuteKillsAProcessThatOutlivesTheWallClockCap(t *testing.T) {
	t.Setenv(sleeperEnv, "1")
	plan := Plan{
		Arm:     ArmTofu,
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
		Arm:     ArmTofu,
		Command: []string{os.Args[0], "-tofu-bench-not-a-flag"},
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
	liveVersionEnv  = "TOFU_LIVE_VERSION"
	liveArmDirEnv   = "TOFU_LIVE_ARM_DIR"
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
	case ArmTofu:
		row, gaps := scoreTree(Row{
			Arm: meta.Arm, Task: meta.Task, Version: meta.Version, Run: meta.Run,
			CLIVersion: meta.CLIVersion, CredentialKind: meta.CredentialKind, Commit: meta.Commit,
			Model: tofuArmModel, EndReason: EndReasonDone,
		}, src, []string{"wall clock, turns, tokens and tool calls: this row was scored from the preserved result tree, not from a turn row, because the run that produced the tree wrote none that survived"})
		return row, gaps, nil
	}
	return Row{}, nil, fmt.Errorf("%q is not an arm this bench measures, want claude, codex or tofu", meta.Arm)
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
	runLiveArm(t, ArmClaude, "TOFU_LIVE_CLAUDE")
}

func TestCodexArmRunsLiveAndProducesARow(t *testing.T) {
	runLiveArm(t, ArmCodex, "TOFU_LIVE_CODEX")
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
	dir := os.Getenv("TOFU_SCORE_DIR")
	if dir == "" {
		t.Skip("scoring: set TOFU_SCORE_DIR to the tree an arm wrote, TOFU_SCORE_ARM to claude, codex or tofu, " +
			"TOFU_LIVE_VERSION to the task version, and TOFU_SCORE_TRANSCRIPT to the raw transcript when there is one. " +
			"This spends nothing, it re-reads a run that already happened.")
	}
	root := repositoryRoot(t)
	version, err := strconv.Atoi(os.Getenv(liveVersionEnv))
	if err != nil {
		t.Fatalf("%s: %v", liveVersionEnv, err)
	}
	execution := Execution{Plan: Plan{Caps: Caps{WallClock: defaultWallClockCap, TurnCap: defaultTurnCap}}}
	transcript := os.Getenv("TOFU_SCORE_TRANSCRIPT")
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
		RunMeta{Arm: Arm(os.Getenv("TOFU_SCORE_ARM")), Task: "hono", Version: version, Run: 1,
			CLIVersion: os.Getenv("TOFU_SCORE_CLI"), CredentialKind: CredentialKindSubscription, Commit: OwnStartCommit(dir)})
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
	stored := filepath.Join(root, filepath.FromSlash(storedTofuV2Row))
	if _, err := os.Stat(stored); err == nil {
		paths = append(paths, stored)
	}
	if len(paths) == 0 {
		t.Skipf("no recorded rows under %s and no stored tofu row at %s", recordedRowsRoot, storedTofuV2Row)
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

const storedTofuV2Row = "bench/harness/testdata/v2-row/row.json"

func TestTofuArmCommandUsesOnlyFlagsRunParses(t *testing.T) {
	root := repositoryRoot(t)
	tofuBin := buildTofu(t, root)
	plan, err := BuildPlan(root, ArmTofu, "hono", 1)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}

	args := append(append([]string{}, plan.Command[1:]...), "--dry-run")
	cmd := exec.Command(tofuBin, args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("tofu %s\nexited %v\n%s", strings.Join(args, " "), err, out)
	}

	bogus := append(append([]string{}, args...), "--wall-clock-cap", "30m")
	cmd = exec.Command(tofuBin, bogus...)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("tofu run accepted --wall-clock-cap, so this test cannot tell a real flag from an invented one\n%s", out)
	}
}

func panicOf(call func()) (message string) {
	defer func() {
		if raised := recover(); raised != nil {
			message = fmt.Sprint(raised)
		}
	}()
	call()
	return ""
}

func everyTurnOutcome() []turn.Outcome {
	var all []turn.Outcome
	for candidate := turn.OutcomeUnset; panicOf(func() { _ = candidate.String() }) == ""; candidate++ {
		all = append(all, candidate)
	}
	return all
}

func TestEndReasonOfNamesEveryTurnOutcome(t *testing.T) {
	readBefore := map[turn.Outcome]EndReason{
		turn.OutcomeUnset:               EndReasonCrash,
		turn.OutcomeStopped:             EndReasonDone,
		turn.OutcomeStepCap:             EndReasonTurnCap,
		turn.OutcomeRetiredCostCap:      EndReasonCrash,
		turn.OutcomeRetiredWallClockCap: EndReasonWallClock,
		turn.OutcomeDecisionCap:         EndReasonTurnCap,
		turn.OutcomeForked:              EndReasonCrash,
		turn.OutcomeError:               EndReasonCrash,
		turn.OutcomeTruncated:           EndReasonTruncated,
	}
	outcomes := everyTurnOutcome()
	if len(outcomes) < len(readBefore) {
		t.Fatalf("the walk found %d outcomes and this table holds %d, so the walk stops short of the enum", len(outcomes), len(readBefore))
	}
	for _, outcome := range outcomes {
		want, pinned := readBefore[outcome]
		if !pinned {
			t.Errorf("outcome %s is new and nothing pins what a row carrying it reads to", outcome)
			continue
		}
		var reason EndReason
		raised := panicOf(func() { reason, _ = endReasonOf(turn.Row{ID: "turn-1", Outcome: outcome}) })
		if raised != "" {
			t.Errorf("outcome %s has no case in endReasonOf: %s", outcome, raised)
			continue
		}
		if reason != want {
			t.Errorf("outcome %s reads to %q, and it read to %q before", outcome, reason, want)
		}
	}
}

func TestARecordedRowCarryingTruncatedReadsToAnEndReason(t *testing.T) {
	dir := t.TempDir()
	writeSessionFile(t, dir, turn.Row{
		ID: "turn-truncated", Schema: turn.SchemaVersion, At: time.Now().Add(-time.Hour),
		Task: "a task", Spend: turn.SpendSubscription, Outcome: turn.OutcomeTruncated,
		Steps: []turn.StepRow{{Index: 1, StopReason: "max_tokens"}},
	})
	row, err := LoadSession(filepath.Join(dir, "turn-truncated.json"))
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if row.Outcome != turn.OutcomeTruncated {
		t.Fatalf("the recorded row read back as %s, so it never carried truncated and this test is not the one it claims to be", row.Outcome)
	}
	reason, gap := endReasonOf(row)
	if reason != EndReasonTruncated || gap != "" {
		t.Fatalf("end reason %q with gap %q, want %q and no gap: the model ran out of output room, nothing capped and nothing crashed", reason, gap, EndReasonTruncated)
	}
}

func writeSessionFile(t *testing.T, dir string, row turn.Row) {
	t.Helper()
	body, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("marshal %s: %v", row.ID, err)
	}
	if err := os.WriteFile(filepath.Join(dir, row.ID+".json"), body, 0o644); err != nil {
		t.Fatalf("write %s: %v", row.ID, err)
	}
}

func forkedChain(t *testing.T, dir string) time.Time {
	t.Helper()
	start := time.Now().Add(-time.Hour)
	writeSessionFile(t, dir, turn.Row{
		ID: "turn-chain", Schema: turn.SchemaVersion, At: start, Task: "a task", Spend: turn.SpendAPIKey,
		Outcome: turn.OutcomeForked, ForkedInto: "turn-chain-f2",
		Steps: []turn.StepRow{{Index: 1}, {Index: 2}},
	})
	writeSessionFile(t, dir, turn.Row{
		ID: "turn-chain-f2", Schema: turn.SchemaVersion, At: start.Add(time.Minute), Task: "a task", Spend: turn.SpendAPIKey,
		Outcome: turn.OutcomeStopped, ForkedFrom: "turn-chain", ForkKind: turn.ForkContinuation,
		Steps: []turn.StepRow{{Index: 3}},
	})
	return start
}

func TestAForkedSessionIsNeverTheOneScored(t *testing.T) {
	dir := t.TempDir()
	forkedChain(t, dir)
	origin, err := LoadSession(filepath.Join(dir, "turn-chain.json"))
	if err != nil {
		t.Fatalf("load the origin session: %v", err)
	}

	session := continuationOf(dir, origin)
	if session.ID != "turn-chain-f2" {
		t.Fatalf("scored session %s, want the continuation turn-chain-f2: a forked session is not the end of the work", session.ID)
	}
	reason, gap := endReasonOf(session)
	if reason != EndReasonDone || gap != "" {
		t.Fatalf("end reason %q with gap %q, want done and no gap", reason, gap)
	}
}

func TestABrokenForkChainIsACrashThatNamesTheMissingSession(t *testing.T) {
	dir := t.TempDir()
	start := forkedChain(t, dir)
	if err := os.Remove(filepath.Join(dir, "turn-chain-f2.json")); err != nil {
		t.Fatalf("remove the continuation, which is what a run killed between the fork and the end leaves behind: %v", err)
	}

	session, err := LatestSession(dir, start.Add(-time.Minute))
	if err != nil {
		t.Fatalf("LatestSession: %v", err)
	}
	if session.ID != "turn-chain" {
		t.Fatalf("scored session %s, want the forked origin, which is all that is on disk", session.ID)
	}
	reason, gap := endReasonOf(session)
	if reason != EndReasonCrash {
		t.Fatalf("end reason %q, want crash: the session that finished the work is not on disk", reason)
	}
	if !strings.Contains(gap, "turn-chain-f2") {
		t.Fatalf("the gap does not name the missing session: %q", gap)
	}
	t.Logf("%s", gap)
}

func TestMeasureBojiFillsTheRowFromAStoredTurnRowAndLedger(t *testing.T) {
	root := repositoryRoot(t)
	transcript := filepath.Join(root, harnessTestdataDir)
	session, err := LoadSession(filepath.Join(transcript, "session.json"))
	if err != nil {
		t.Skipf("no stored transcript at %s: the recorded tofu run is not in the repository yet and %s is not in this ticket's owns, see the report: %v", transcript, transcript, err)
	}

	meta := RunMeta{Arm: ArmTofu, Task: "hono", Version: 1, Run: 1, CredentialKind: CredentialKindKey}
	src := Sources{
		LedgerDir:   transcript,
		ArmDir:      ArmDir(root, ArmTofu, "hono"),
		BunBin:      "bun",
		CheckerPath: CheckerPath(root, 1),
		StartCommit: OwnStartCommit(ArmDir(root, ArmTofu, "hono")),
	}
	row, gaps := MeasureTofu(session, src, meta)

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

func buildTofu(t *testing.T, root string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	bin := filepath.Join(t.TempDir(), "tofu.exe")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/tofu")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("cmd/tofu does not build right now, so the flag list cannot be checked against its parser: %v\n%s", err, out)
	}
	return bin
}
