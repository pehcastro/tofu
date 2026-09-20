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
	"time"

	"tofu/bench/harness"
	"tofu/internal/judge/jev/wire/openrouter"
	"tofu/internal/sys"
	"tofu/internal/turn"
)

type harnessOpts struct {
	arm        harness.Arm
	task       string
	version    int
	offline    bool
	transcript string
	run        int
	tofuBin    string
}

const harnessTranscriptDir = "bench/harness/testdata/boji-1"

func parseHarnessArgs(args []string) (harnessOpts, error) {
	opts := harnessOpts{task: "hono", version: 1, transcript: harnessTranscriptDir, run: 1}
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
		case "--run":
			opts.run, err = nextInt(args, &i, arg)
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
	if opts.arm != harness.ArmTofu {
		if err := harness.Fprint(out, plan); err != nil {
			return fail(errOut, "harness", err)
		}
		_, _ = fmt.Fprintf(out, "the %s arm printed its plan and ran nothing: executing it spends an account the owner has not approved for this bench\n", opts.arm)
		return exitOK
	}

	if !opts.offline {
		bin, cleanup, err := tofuUnderTest(opts.tofuBin)
		if err != nil {
			return fail(errOut, "harness", err)
		}
		defer cleanup()
		plan.Command[0] = bin
	}

	session, execution, err := harnessSession(out, plan, opts)
	if err != nil {
		return fail(errOut, "harness", err)
	}

	state, err := sys.ProjectStateDir()
	if err != nil {
		return fail(errOut, "harness", err)
	}
	ledgerDir := filepath.Join(state, "log")
	if opts.offline {
		ledgerDir = opts.transcript
	}
	sources := harness.Sources{
		LedgerDir:   ledgerDir,
		ArmDir:      plan.Dir,
		BunBin:      "bun",
		CheckerPath: harness.CheckerPath(".", opts.version),
		StartCommit: harness.OwnStartCommit(plan.Dir),
	}
	meta := harness.RunMeta{
		Arm: opts.arm, Task: opts.task, Version: opts.version, Run: opts.run,
		CLIVersion: sys.Version(), Commit: sys.BuildRevision(),
	}

	row, gaps := harness.MeasureTofu(session, sources, meta)
	renderHarnessRow(out, row, gaps, execution, opts, ledgerDir)
	_, _ = fmt.Fprint(out, harness.Render([]harness.Row{row}))
	return exitOK
}

func printEveryPlan(out io.Writer, opts harnessOpts) error {
	for _, arm := range []harness.Arm{harness.ArmTofu, harness.ArmClaude, harness.ArmCodex} {
		plan, err := harness.BuildPlan(".", arm, opts.task, opts.version)
		if err != nil {
			return err
		}
		if err := harness.Fprint(out, plan); err != nil {
			return err
		}
	}
	return nil
}

func harnessSession(out io.Writer, plan harness.Plan, opts harnessOpts) (turn.Row, harness.Execution, error) {
	if opts.offline {
		path := filepath.Join(opts.transcript, "session.json")
		session, err := harness.LoadSession(path)
		if err != nil {
			return turn.Row{}, harness.Execution{}, fmt.Errorf("the offline arm replays a stored transcript and %s is not readable, name another with --transcript: %w", path, err)
		}
		return session, harness.Execution{Plan: plan, Start: session.At, End: session.At, EndReason: harness.EndReasonDone}, nil
	}
	_, _ = fmt.Fprintf(out, "running: %s\n", harness.Shell(plan.Command))
	execution, err := harness.Execute(context.Background(), plan)
	if err != nil {
		return turn.Row{}, harness.Execution{}, err
	}
	_, _ = fmt.Fprint(out, execution.Stdout, execution.Stderr)
	state, err := sys.ProjectStateDir()
	if err != nil {
		return turn.Row{}, execution, err
	}
	session, err := harness.LatestSession(filepath.Join(state, "sessions"), execution.Start)
	return session, execution, err
}

func renderHarnessRow(out io.Writer, row harness.Row, gaps []string, execution harness.Execution, opts harnessOpts, ledgerDir string) {
	host, err := os.Hostname()
	if err != nil {
		host = "unknown"
	}
	source := fmt.Sprintf("live, %s of wall clock in the runner, exit code %d, runner end reason %s",
		execution.Elapsed().Round(time.Millisecond), execution.ExitCode, execution.EndReason)
	if opts.offline {
		source = "offline, replayed from " + opts.transcript + ", no arm was executed and no model was called"
	}
	passed, total := harness.ChecklistScore(row)
	modelSpend := "subscription quota, no money, so there is no model dollar figure"
	if row.ModelDollars != nil {
		modelSpend = fmt.Sprintf("api key $%.6f", *row.ModelDollars)
	}
	caps := execution.Plan.Caps

	_, _ = fmt.Fprintf(out, "\nROW %s %s v%d run%d\n", row.Arm, row.Task, row.Version, row.Run)
	_, _ = fmt.Fprintf(out, "source: %s\n", source)
	_, _ = fmt.Fprintf(out, "machine: %s. model credential kind: %s. date: %s. commit: %s. cli: %s\n",
		host, row.CredentialKind, row.Start.Format("2006-01-02"), row.Commit, row.CLIVersion)
	_, _ = fmt.Fprintf(out, "model: %s. judge wire: %s. judge ledger: %s\n", row.Model, openrouter.Name, ledgerDir)
	_, _ = fmt.Fprintf(out, "checklist: %d/%d graded items\n", passed, total)
	_, _ = fmt.Fprintf(out, "model spend: %s. jev decisions: $%.6f on the openrouter key\n", modelSpend, row.JudgeDollars)
	_, _ = fmt.Fprintf(out, "wall clock: %d ms. turns: %d. end reason: %s\n",
		row.WallClockMS, row.Turns, row.EndReason)
	_, _ = fmt.Fprintf(out, "tokens: %d in, %d out\n", row.BilledInput, row.BilledOutput)
	_, _ = fmt.Fprintf(out, "tool calls: read %d, write %d, shell %d, other %d, failed %d\n",
		row.ToolCalls.Read, row.ToolCalls.Write, row.ToolCalls.Shell, row.ToolCalls.Other, row.ToolCalls.Failed)
	_, _ = fmt.Fprintf(out, "caps, unset rather than measured, nobody has given a number: wall clock %s, turns %d\n",
		caps.WallClock, caps.TurnCap)
	for _, gate := range row.Gates {
		_, _ = fmt.Fprintf(out, "gate %s: %s %s\n", gate.Name, gate.Status, firstLine(gate.Reason))
	}
	for _, item := range row.Checklist {
		_, _ = fmt.Fprintf(out, "checklist %s: %t\n", item.Item, item.Passed)
	}
	for _, gap := range gaps {
		_, _ = fmt.Fprintf(out, "gap: %s\n", gap)
	}
	_, _ = fmt.Fprintln(out)
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	if len(line) > 160 {
		return line[:160] + " ..."
	}
	return line
}
