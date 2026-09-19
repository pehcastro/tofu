package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	benchapi "boji/bench/api"
	benchcost "boji/bench/cost"
	"boji/bench/harness"
	"boji/bench/report"
	benchturn "boji/bench/turn"
	benchwording "boji/bench/wording"
	"boji/internal/judge/jev"
	"boji/internal/judge/jev/wire/openrouter"
	"boji/internal/sys"
	"boji/internal/turn"
)

func benchVerb(args []string, out, errOut io.Writer) int {
	if len(args) == 0 {
		return benchFail(errOut, "bench", errors.New(`a target is required, e.g. "boji bench api"`))
	}
	target, rest := args[0], args[1:]
	if target == "harness" {
		return benchHarness(out, errOut, rest)
	}
	offline := false
	for _, arg := range rest {
		if arg != "--offline" {
			return benchFail(errOut, "bench", fmt.Errorf("unknown argument %q", arg))
		}
		offline = true
	}
	switch target {
	case "api":
		return benchAPI(out, errOut, offline)
	case "cost":
		return benchCost(out, errOut, offline)
	case "wording":
		return benchWording(out, errOut, offline)
	case "turn":
		return benchTurn(out, errOut, offline)
	}
	return benchFail(errOut, "bench", fmt.Errorf("unknown target %q", target))
}

func benchAPI(out, errOut io.Writer, offline bool) int {
	if offline {
		_, _ = fmt.Fprintln(out, "boji bench api: skipped, --offline was set, no network call was made")
		return exitOK
	}

	key, err := jev.Key(".env")
	if err != nil {
		return benchFail(errOut, "api", err)
	}
	wire, err := benchapi.NewWire(key)
	if err != nil {
		return benchFail(errOut, "api", err)
	}
	result, err := benchapi.Run(context.Background(), wire)
	if err != nil {
		return benchFail(errOut, "api", err)
	}

	body := report.Render(result, benchConditions(result.GeneratedAt))

	if err := os.MkdirAll("bench/report", 0o755); err != nil {
		return benchFail(errOut, "api", err)
	}
	path := "bench/report/" + report.Filename(result.GeneratedAt)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return benchFail(errOut, "api", err)
	}

	_, _ = fmt.Fprint(out, body)
	return exitOK
}

func benchCost(out, errOut io.Writer, offline bool) int {
	if offline {
		_, _ = fmt.Fprintln(out, "boji bench cost: skipped, --offline was set, no network call was made")
		return exitOK
	}

	key, err := jev.Key(".env")
	if err != nil {
		return benchFail(errOut, "cost", err)
	}
	result, err := benchcost.Run(context.Background(), key)
	if err != nil {
		return benchFail(errOut, "cost", err)
	}

	body := benchcost.Render(result, benchConditions(result.GeneratedAt))

	if err := os.MkdirAll("bench/cost", 0o755); err != nil {
		return benchFail(errOut, "cost", err)
	}
	path := "bench/cost/" + benchcost.Filename(result.GeneratedAt)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return benchFail(errOut, "cost", err)
	}

	_, _ = fmt.Fprint(out, body)
	return exitOK
}

func benchWording(out, errOut io.Writer, offline bool) int {
	if offline {
		_, _ = fmt.Fprintln(out, "boji bench wording: skipped, --offline was set, no network call was made")
		return exitOK
	}

	key, err := jev.Key(".env")
	if err != nil {
		return benchFail(errOut, "wording", err)
	}
	result, err := benchwording.Run(context.Background(), key)
	if err != nil {
		return benchFail(errOut, "wording", err)
	}

	body := benchwording.Render(result, benchConditions(result.GeneratedAt))

	if err := os.MkdirAll("bench/wording", 0o755); err != nil {
		return benchFail(errOut, "wording", err)
	}
	path := "bench/wording/" + benchwording.Filename(result.GeneratedAt)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return benchFail(errOut, "wording", err)
	}

	_, _ = fmt.Fprint(out, body)
	return exitOK
}

func benchTurn(out, errOut io.Writer, offline bool) int {
	if offline {
		_, _ = fmt.Fprintln(out, "boji bench turn: skipped, --offline was set, no network call was made")
		return exitOK
	}

	key, err := jev.Key(".env")
	if err != nil {
		return benchFail(errOut, "turn", err)
	}
	result, err := benchturn.Run(context.Background(), key)
	if err != nil {
		return benchFail(errOut, "turn", err)
	}

	body := benchturn.Render(result, benchConditions(result.GeneratedAt))

	if err := os.MkdirAll("bench/turn", 0o755); err != nil {
		return benchFail(errOut, "turn", err)
	}
	path := "bench/turn/" + benchturn.Filename(result.GeneratedAt)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return benchFail(errOut, "turn", err)
	}

	_, _ = fmt.Fprint(out, body)
	return exitOK
}

func benchConditions(generatedAt time.Time) report.Conditions {
	host, err := os.Hostname()
	if err != nil {
		host = "unknown"
	}
	return report.Conditions{
		Machine:        host,
		CredentialKind: "key",
		Wire:           openrouter.Name,
		Date:           generatedAt.Format("2006-01-02"),
	}
}

func benchFail(errOut io.Writer, target string, err error) int {
	_, _ = fmt.Fprintf(errOut, "boji bench %s: %v\n", target, err)
	return exitUsage
}

type harnessOpts struct {
	arm        harness.Arm
	task       string
	version    int
	offline    bool
	transcript string
	run        int
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
		default:
			err = fmt.Errorf("unknown argument %q", arg)
		}
		if err != nil {
			return harnessOpts{}, err
		}
	}
	if opts.offline && opts.arm == "" {
		opts.arm = harness.ArmBoji
	}
	return opts, nil
}

func knownArm(name string) error {
	switch harness.Arm(name) {
	case harness.ArmBoji, harness.ArmClaude, harness.ArmCodex:
		return nil
	}
	return fmt.Errorf("unknown arm %q, the arms are boji, claude and codex", name)
}

func benchHarness(out, errOut io.Writer, args []string) int {
	opts, err := parseHarnessArgs(args)
	if err != nil {
		return benchFail(errOut, "harness", err)
	}
	if opts.arm == "" {
		if err := printEveryPlan(out, opts); err != nil {
			return benchFail(errOut, "harness", err)
		}
		_, _ = fmt.Fprintln(out, "no arm was named, so nothing ran. Name one with --arm.")
		return exitOK
	}

	plan, err := harness.BuildPlan(".", opts.arm, opts.task, opts.version)
	if err != nil {
		return benchFail(errOut, "harness", err)
	}
	if opts.arm != harness.ArmBoji {
		if err := harness.Fprint(out, plan); err != nil {
			return benchFail(errOut, "harness", err)
		}
		_, _ = fmt.Fprintf(out, "the %s arm printed its plan and ran nothing: executing it spends an account the owner has not approved for this bench\n", opts.arm)
		return exitOK
	}

	self, err := os.Executable()
	if err != nil {
		return benchFail(errOut, "harness", err)
	}
	plan.Command[0] = self

	session, execution, err := harnessSession(out, plan, opts)
	if err != nil {
		return benchFail(errOut, "harness", err)
	}

	state, err := sys.ProjectStateDir()
	if err != nil {
		return benchFail(errOut, "harness", err)
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
		CLIVersion: sys.Version(), CredentialKind: harness.CredentialKindKey, Commit: sys.BuildRevision(),
	}

	row, gaps := harness.MeasureBoji(session, sources, meta)
	renderHarnessRow(out, row, gaps, execution, opts, ledgerDir)
	_, _ = fmt.Fprint(out, harness.Render([]harness.Row{row}))
	return exitOK
}

func printEveryPlan(out io.Writer, opts harnessOpts) error {
	for _, arm := range []harness.Arm{harness.ArmBoji, harness.ArmClaude, harness.ArmCodex} {
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
	dollars := 0.0
	if row.Dollars != nil {
		dollars = *row.Dollars
	}
	caps := execution.Plan.Caps

	_, _ = fmt.Fprintf(out, "\nROW %s %s v%d run%d\n", row.Arm, row.Task, row.Version, row.Run)
	_, _ = fmt.Fprintf(out, "source: %s\n", source)
	_, _ = fmt.Fprintf(out, "machine: %s. credential kind: %s. wire: %s. date: %s. commit: %s. cli: %s\n",
		host, row.CredentialKind, openrouter.Name, row.Start.Format("2006-01-02"), row.Commit, row.CLIVersion)
	_, _ = fmt.Fprintf(out, "model: %s. judge ledger: %s\n", row.Model, ledgerDir)
	_, _ = fmt.Fprintf(out, "checklist: %d/%d graded items\n", passed, total)
	_, _ = fmt.Fprintf(out, "wall clock: %d ms. turns: %d. dollars: $%.6f. end reason: %s\n",
		row.WallClockMS, row.Turns, dollars, row.EndReason)
	_, _ = fmt.Fprintf(out, "tokens: %d in, %d out\n", row.BilledInput, row.BilledOutput)
	_, _ = fmt.Fprintf(out, "tool calls: read %d, write %d, shell %d, other %d, failed %d\n",
		row.ToolCalls.Read, row.ToolCalls.Write, row.ToolCalls.Shell, row.ToolCalls.Other, row.ToolCalls.Failed)
	_, _ = fmt.Fprintf(out, "caps, unset rather than measured, nobody has given a number: wall clock %s, dollars $%.2f, turns %d\n",
		caps.WallClock, caps.DollarCap, caps.TurnCap)
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
