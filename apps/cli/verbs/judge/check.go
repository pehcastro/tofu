package judge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"tofu/apps/cli/output"
	"tofu/apps/cli/page"
	"tofu/apps/cli/wiring"
	"tofu/internal/judge/gate"
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/state"
	"tofu/internal/models/classifiers/core"
	"tofu/internal/sys"
	"tofu/library/questions"
)

const runGatePoint = questions.ToolGatePin

const CheckUsage = `tofu check "<command>" [--quiet] [--json]`

type checkOpts struct {
	command string
	quiet   bool
}

type checkReport struct {
	ID      string         `json:"id"`
	Command string         `json:"command"`
	Verdict ledger.Verdict `json:"verdict"`
}

func CheckVerb(args []string, out, errOut io.Writer) int {
	o := output.Verb{Name: "check", UsageLine: CheckUsage, AsJSON: output.JSONAsked(args), Out: out, ErrOut: errOut}
	opts, err := parseCheckArgs(args)
	if err != nil {
		return o.Usage(err)
	}
	row, err := checkedRow(opts.command)
	if err != nil {
		return wiring.Failed(o, err)
	}
	if opts.quiet {
		return output.ExitOK
	}
	return o.Done(true, checkReport{ID: row.ID, Command: opts.command, Verdict: row.Verdict}, func(page cli.Page) []string {
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
	key, err := wiring.GateKey()
	if err != nil {
		return ledger.Row{}, err
	}
	client, err := wiring.JevClientOn(key, wiring.OneCallAtATime)
	if err != nil {
		return ledger.Row{}, err
	}
	return RunCheck(context.Background(), client, rulePath, command)
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
		case arg == output.JSONFlag:
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

func RunCheck(ctx context.Context, client *jev.Client, rulePath, command string) (ledger.Row, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return ledger.Row{}, err
	}
	set, err := wiring.ResolveLibrary(runGatePoint, "")
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
		r, _, err = wiring.LoadRulePoint(runGatePoint, "")
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
	return wiring.AppendRow(rawState, set, wiring.RowInput{
		Decision:     &decision,
		Answers:      wiring.ToLedgerAnswers(set.QuestionsVersion, decision.Answers),
		StateBuilder: builderVersion,
		Fingerprint:  state.FingerprintOf(call),
	})
}
