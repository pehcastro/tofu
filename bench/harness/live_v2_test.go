package harness

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"tofu/internal/sys"
)

const (
	liveEnv           = "TOFU_LIVE"
	binaryEnv         = "TOFU_BIN"
	v2SessionTestdata = "testdata/v2-session"
	v2BeforeTestdata  = "testdata/v2-before"
)

func TestV2BojiArmRunsLiveAndProducesARow(t *testing.T) {
	if os.Getenv(liveEnv) != "1" {
		t.Skip("live: this runs the tofu arm against the playground and spends the subscription. Set " +
			liveEnv + "=1 and " + binaryEnv + " to a built tofu binary to run it.")
	}
	binary := os.Getenv(binaryEnv)
	if binary == "" {
		t.Fatal(binaryEnv + " is unset: the arm is driven by a built tofu binary, and building one here would be a tree-wide check")
	}
	root := repositoryRoot(t)
	sessionDir := filepath.Join(root, "bench", "harness", v2SessionTestdata)
	t.Chdir(root)

	plan, err := BuildPlan(".", ArmTofu, "hono", 2)
	if err != nil {
		t.Fatal(err)
	}
	plan.Command[0] = binary
	t.Logf("running: %s", Shell(plan.Command))

	execution, err := Execute(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("arm stdout:\n%s", execution.Stdout)
	if execution.Stderr != "" {
		t.Logf("arm stderr:\n%s", execution.Stderr)
	}

	state, err := sys.ProjectStateDir()
	if err != nil {
		t.Fatal(err)
	}
	recorded, err := LatestSession(filepath.Join(state, "sessions"), execution.Start)
	if err != nil {
		t.Fatal(err)
	}
	if err := StoreTranscript(sessionDir, recorded); err != nil {
		t.Fatal(err)
	}

	ledgerDir := filepath.Join(state, "log")
	src := Sources{
		LedgerDir:   ledgerDir,
		ArmDir:      plan.Dir,
		BunBin:      "bun",
		CheckerPath: CheckerPath(".", 2),
		StartCommit: OwnStartCommit(plan.Dir),
	}
	meta := RunMeta{
		Arm: ArmTofu, Task: "hono", Version: 2, Run: 1,
		CLIVersion: sys.Version(), Commit: sys.BuildRevision(),
	}
	row, gaps := MeasureTofu(recorded, src, meta)
	t.Logf("\n%s\n%s", Detail(row, gaps, execution, ledgerDir), Render([]Row{row}))

	if row.Turns == 0 {
		t.Fatal("the run produced a turn row with no steps, so nothing was measured")
	}
	t.Logf("the recorded turn is stored at %s, and TestTransformArmsOverTheV2Session counts its writes", sessionDir)
}
