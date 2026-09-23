package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"tofu/bench/harness"
	"tofu/internal/sys"
)

type harnessOpts struct {
	arm        harness.Arm
	task       string
	version    int
	offline    bool
	transcript string
	repeats    int
	tofuBin    string
}

const harnessTranscriptDir = "bench/harness/testdata/tofu-v1"

func parseHarnessArgs(args []string) (harnessOpts, error) {
	opts := harnessOpts{task: "hono", version: 1, transcript: harnessTranscriptDir, repeats: 1}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		var err error
		switch arg {
		case "--offline":
			opts.offline = true
		case "--arm":
			var name string
			if name, err = nextArg(args, &i, arg); err == nil {
				opts.arm, err = harness.Arm(name), knownArm(name)
			}
		case "--task":
			opts.task, err = nextArg(args, &i, arg)
		case "--version":
			opts.version, err = nextInt(args, &i, arg)
		case "--repeats":
			opts.repeats, err = nextInt(args, &i, arg)
		case "--transcript":
			opts.transcript, err = nextArg(args, &i, arg)
		case "--tofu":
			opts.tofuBin, err = nextArg(args, &i, arg)
		default:
			err = fmt.Errorf("unknown argument %q", arg)
		}
		if err != nil {
			return harnessOpts{}, err
		}
	}
	if opts.offline && opts.arm == "" {
		opts.arm = harness.ArmTofu
	}
	return opts, nil
}

func knownArm(name string) error {
	switch harness.Arm(name) {
	case harness.ArmTofu, harness.ArmClaude, harness.ArmCodex:
		return nil
	}
	return fmt.Errorf("unknown arm %q, the arms are tofu, claude and codex", name)
}

func tofuUnderTest(named string) (path string, cleanup func(), err error) {
	if named != "" {
		abs, err := filepath.Abs(named)
		return abs, func() {}, err
	}
	dir, err := os.MkdirTemp("", "tofu-bench-arm")
	if err != nil {
		return "", func() {}, err
	}
	bin := filepath.Join(dir, "tofu")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	build := exec.Command("go", "build", "-o", bin, "./cmd/tofu")
	if out, err := build.CombinedOutput(); err != nil {
		_ = os.RemoveAll(dir)
		return "", func() {}, fmt.Errorf("the tofu arm measures the tofu in this tree, so the bench builds ./cmd/tofu, and that failed. Name a built binary with --tofu to measure one instead: %w\n%s", err, out)
	}
	return bin, func() { _ = os.RemoveAll(dir) }, nil
}

func benchHarness(out, errOut io.Writer, args []string) int {
	opts, err := parseHarnessArgs(args)
	if err != nil {
		return fail(errOut, "harness", err)
	}
	if opts.arm == "" {
		if err := printEveryPlan(out, opts); err != nil {
			return fail(errOut, "harness", err)
		}
		_, _ = fmt.Fprintln(out, "no arm was named, so nothing ran. Name one with --arm.")
		return exitOK
	}

	plan, err := harness.BuildPlan(".", opts.arm, opts.task, opts.version)
	if err != nil {
		return fail(errOut, "harness", err)
	}
	plan.Repeats = opts.repeats
	if opts.arm != harness.ArmTofu {
		if err := harness.Fprint(out, plan); err != nil {
			return fail(errOut, "harness", err)
		}
		_, _ = fmt.Fprintf(out, "the %s arm printed its plan and ran nothing: executing it spends an account the owner has not approved for this bench\n", opts.arm)
		return exitOK
	}

	if opts.offline {
		return replayOneRepeat(out, errOut, plan, opts)
	}

	bin, cleanup, err := tofuUnderTest(opts.tofuBin)
	if err != nil {
		return fail(errOut, "harness", err)
	}
	defer cleanup()
	plan.Command[0] = bin

	state, err := sys.ProjectStateDir()
	if err != nil {
		return fail(errOut, "harness", err)
	}
	ledgerDir := filepath.Join(state, "log")

	measure := func(execution harness.Execution, meta harness.RunMeta) (harness.Row, []string, error) {
		meta.CLIVersion, meta.Commit = sys.Version(), sys.BuildRevision()
		session, err := harness.LatestSession(filepath.Join(state, "sessions"), execution.Start)
		if err != nil {
			return harness.Row{}, nil, err
		}
		row, gaps := harness.MeasureTofu(session, harnessSources(plan.Dir, ledgerDir, opts.version), meta)
		return row, gaps, nil
	}

	_, _ = fmt.Fprintf(out, "running %s\neach of %d repeats after staging the v%d seed into %s\n",
		harness.Shell(plan.Command), plan.Repeats, plan.Version, plan.Dir)
	repeats, err := harness.RunRepeats(context.Background(), ".", plan, measure)
	if err != nil {
		return fail(errOut, "harness", err)
	}
	rows := make([]harness.Row, 0, len(repeats))
	for _, repeat := range repeats {
		_, _ = fmt.Fprint(out, repeat.Execution.Stdout, repeat.Execution.Stderr)
		_, _ = fmt.Fprintln(out, "\n"+harness.Detail(repeat.Row, repeat.Gaps, repeat.Execution, ledgerDir, harness.LiveSource(repeat.Execution)))
		rows = append(rows, repeat.Row)
	}
	_, _ = fmt.Fprint(out, harness.Render(rows))
	return exitOK
}

func harnessSources(armDir, ledgerDir string, version int) harness.Sources {
	return harness.Sources{
		LedgerDir:   ledgerDir,
		ArmDir:      armDir,
		BunBin:      "bun",
		CheckerPath: harness.CheckerPath(".", version),
		StartCommit: harness.OwnStartCommit(armDir),
	}
}

func replayOneRepeat(out, errOut io.Writer, plan harness.Plan, opts harnessOpts) int {
	path := filepath.Join(opts.transcript, "session.json")
	session, err := harness.LoadSession(path)
	if err != nil {
		return fail(errOut, "harness", fmt.Errorf("the offline arm replays a stored transcript and %s is not readable, name another with --transcript: %w", path, err))
	}
	meta := plan.Runs()[0]
	meta.CLIVersion, meta.Commit = sys.Version(), sys.BuildRevision()
	row, gaps := harness.MeasureTofu(session, harnessSources(plan.Dir, opts.transcript, opts.version), meta)
	execution := harness.Execution{Plan: plan, Start: session.At, End: session.At, EndReason: harness.EndReasonDone}
	source := "offline, replayed from " + opts.transcript + ", no arm was executed and no model was called"
	_, _ = fmt.Fprintln(out, "\n"+harness.Detail(row, gaps, execution, opts.transcript, source))
	_, _ = fmt.Fprint(out, harness.Render([]harness.Row{row}))
	return exitOK
}

func printEveryPlan(out io.Writer, opts harnessOpts) error {
	var apart []string
	for _, arm := range []harness.Arm{harness.ArmTofu, harness.ArmClaude, harness.ArmCodex} {
		plan, err := harness.BuildPlan(".", arm, opts.task, opts.version)
		if err != nil {
			return err
		}
		if err := harness.Fprint(out, plan); err != nil {
			return err
		}
		if plan.Effort != harness.AskedEffort {
			apart = append(apart, fmt.Sprintf("%s runs at %s: %s", arm, plan.Effort, plan.EffortSetBy))
		}
	}
	if len(apart) == 0 {
		_, _ = fmt.Fprintf(out, "every arm was asked for thinking effort %s on its own command line, so the three are on a par\n", harness.AskedEffort)
		return nil
	}
	_, _ = fmt.Fprintf(out, "thinking effort %s was asked for and %s\n", harness.AskedEffort, strings.Join(apart, "; "))
	return nil
}
