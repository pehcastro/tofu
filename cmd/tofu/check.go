package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"tofu/internal/judge/jev"
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/policy"
	"tofu/internal/judge/state"
	"tofu/internal/sys"
)

type checkOpts struct {
	command string
	quiet   bool
}

func checkVerb(args []string, out, errOut io.Writer) int {
	opts, err := parseCheckArgs(args)
	if err != nil {
		return checkFail(errOut, err)
	}

	catalogDir, err := sys.CatalogDir()
	if err != nil {
		return checkFail(errOut, err)
	}
	policyPath := sys.Join(catalogDir, "policy", runGatePoint+".yaml")

	key, err := jev.Key(".env")
	if err != nil {
		return checkFail(errOut, err)
	}
	client, err := jevClientOn(key)
	if err != nil {
		return checkFail(errOut, err)
	}

	row, err := runCheck(context.Background(), client, policyPath, opts.command)
	if err != nil {
		return checkFail(errOut, err)
	}
	if !opts.quiet {
		_, _ = fmt.Fprintf(out, "%s  %s\n", row.ID, colorVerdict(row.Verdict, isTerminalWriter(out)))
	}
	return exitOK
}

func checkFail(errOut io.Writer, err error) int {
	_, _ = fmt.Fprintf(errOut, "tofu check: %v\n", err)
	return exitUsage
}

func parseCheckArgs(args []string) (checkOpts, error) {
	var opts checkOpts
	for _, arg := range args {
		if arg == "--quiet" {
			opts.quiet = true
			continue
		}
		if opts.command != "" {
			return checkOpts{}, fmt.Errorf("tofu check takes one command, got %q and %q", opts.command, arg)
		}
		opts.command = arg
	}
	if opts.command == "" {
		return checkOpts{}, errors.New("tofu check needs a command")
	}
	return opts, nil
}

func runCheck(ctx context.Context, client *jev.Client, policyPath, command string) (ledger.Row, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return ledger.Row{}, err
	}
	set, err := resolveCatalog(runGatePoint)
	if err != nil {
		return ledger.Row{}, err
	}
	call := state.ToolGateInput{
		Agent:      "owner-shell",
		Tool:       "bash",
		Input:      map[string]any{"command": command},
		Cwd:        cwd,
		ProjectDir: cwd,
	}
	built, builderVersion, err := state.BuildToolGateV3(call)
	if err != nil {
		return ledger.Row{}, err
	}
	rawState := json.RawMessage(built)

	pol, err := policy.Load(policyPath)
	if err != nil {
		return ledger.Row{}, err
	}
	set.Policy, set.Mode = &pol, policy.ModeShadow

	decision, err := client.Ask(ctx, jev.Request{State: rawState, Questions: set.Questions})
	if err != nil {
		return ledger.Row{}, err
	}
	return appendRow(rawState, set, rowInput{
		decision:     &decision,
		answers:      toLedgerAnswers(set.QuestionsVersion, decision.Answers),
		stateBuilder: builderVersion,
		fingerprint:  state.FingerprintOf(call),
	})
}
