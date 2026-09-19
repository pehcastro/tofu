package harness

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
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

func TestExecuteRefusesTheClaudeAndCodexArms(t *testing.T) {
	root := repositoryRoot(t)
	for _, arm := range []Arm{ArmClaude, ArmCodex} {
		plan, err := BuildPlan(root, arm, "hono", 1)
		if err != nil {
			t.Fatalf("%s: BuildPlan: %v", arm, err)
		}
		if _, err := Execute(context.Background(), plan); err == nil {
			t.Fatalf("%s: Execute returned no error, the runner must never spend that account", arm)
		}
	}
}

func TestExecuteStartsNoProcessForTheClaudeAndCodexArms(t *testing.T) {
	root := repositoryRoot(t)
	dir := t.TempDir()
	marker := filepath.Join(dir, "marker.txt")
	for _, name := range []string{"claude.cmd", "codex.cmd"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("@echo ran > \""+marker+"\"\r\n"), 0o755); err != nil {
			t.Fatalf("write sentinel %s: %v", name, err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	for _, arm := range []Arm{ArmClaude, ArmCodex} {
		plan, err := BuildPlan(root, arm, "hono", 1)
		if err != nil {
			t.Fatalf("%s: BuildPlan: %v", arm, err)
		}
		_, _ = Execute(context.Background(), plan)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a sentinel executable ran: Execute started a real process for an arm it must refuse")
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat marker: %v", err)
	}
}

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
