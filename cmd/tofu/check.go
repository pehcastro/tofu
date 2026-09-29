package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"tofu/interface/cli"
	"tofu/internal/judge/gate"
	"tofu/internal/judge/jev"
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/state"
	"tofu/internal/sys"
)

const checkUsage = `tofu check "<command>" [--quiet] [--json]`

type checkOpts struct {
	command string
	quiet   bool
	asJSON  bool
}

type checkReport struct {
	ID      string         `json:"id"`
	Command string         `json:"command"`
	Verdict ledger.Verdict `json:"verdict"`
}

func checkVerb(args []string, out, errOut io.Writer) int {
	o := verbOutput{verb: "check", usageLine: checkUsage, out: out, errOut: errOut}
	opts, err := parseCheckArgs(args)
	if err != nil {
		return o.usage(err)
	}
	o.asJSON = opts.asJSON
	row, err := checkedRow(opts.command)
	if err != nil {
		return failed(o, err)
	}
	if opts.quiet {
		return exitOK
	}
	return o.done(true, checkReport{ID: row.ID, Command: opts.command, Verdict: row.Verdict}, func(page cli.Page) []string {
		return page.Rows([]cli.Row{verdictRow(row.Verdict, opts.command, row.ID)})
	})
}

func checkedRow(command string) (ledger.Row, error) {
	libraryDir, err := sys.LibraryDir()
	if err != nil {
		return ledger.Row{}, err
	}
	rulePath := ""
	isDir, err := sys.IsDir(libraryDir)
	if err != nil {
		return ledger.Row{}, err
	}
	if isDir {
		found, err := gate.FindRule(os.DirFS(libraryDir), runGatePoint)
		if err != nil {
			return ledger.Row{}, err
		}
		rulePath = sys.Join(libraryDir, filepath.FromSlash(found))
	}
	key, err := gateKey()
	if err != nil {
		return ledger.Row{}, err
	}
	client, err := jevClientOn(key, oneCallAtATime)
	if err != nil {
		return ledger.Row{}, err
	}
	return runCheck(context.Background(), client, rulePath, command)
}

func verdictRow(v ledger.Verdict, command, id string) cli.Row {
	row := cli.Row{Cells: []string{string(v), command}, Detail: id}
	switch v {
	case ledger.VerdictAllow:
		row.Mark = cli.Done
	case ledger.VerdictAsk:
		row.Mark = cli.Warn
	case ledger.VerdictDeny:
		row.Mark = cli.Fail
	case ledger.VerdictUnset:
		row.Mark, row.Cells[0] = cli.Idle, "no verdict"
	default:
		panic("tofu check: unknown verdict " + string(v))
	}
	return row
}

func parseCheckArgs(args []string) (checkOpts, error) {
	var opts checkOpts
	for _, arg := range args {
		switch {
		case arg == "--quiet":
			opts.quiet = true
		case arg == jsonFlag:
			opts.asJSON = true
		case strings.HasPrefix(arg, "--"):
			return checkOpts{}, fmt.Errorf("unknown argument %q", arg)
		case opts.command != "":
			return checkOpts{}, fmt.Errorf("takes one command, got %q and %q", opts.command, arg)
		default:
			opts.command = arg
		}
	}
	if opts.command == "" {
		return checkOpts{}, errors.New("needs a command")
	}
	return opts, nil
}

func runCheck(ctx context.Context, client *jev.Client, rulePath, command string) (ledger.Row, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return ledger.Row{}, err
	}
	set, err := resolveLibrary(runGatePoint, "")
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

	var r gate.Rule
	if rulePath == "" {
		r, _, err = loadRulePoint(runGatePoint, "")
	} else {
		r, err = gate.Load(rulePath)
	}
	if err != nil {
		return ledger.Row{}, err
	}
	set.Rule, set.Mode = &r, gate.ModeShadow

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
