package main

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/internal/host"
	"tofu/internal/llm/models"
	"tofu/internal/llm/quota"
	sessionstore "tofu/internal/session"
	"tofu/internal/turn"
)

const serveUsage = `usage: tofu serve --stdio [--dir PATH] [--cassette PATH]
       tofu serve --schema

serves one project to a frontend over standard input and output: JSON-RPC 2.0,
one object a line, in the protocol tofu.host/1. Standard error carries logs and
nothing else. Closing standard input stops the running turn and ends tofu.

--schema prints the JSON Schema of every line tofu writes, with the lines it
reads under $defs.clientMessage.

--cassette PATH answers every model call from a recorded cassette, as tofu
drive does, so a frontend is built and tested with no model and no network.
TOFU_DRIVE_CASSETTE names it when --cassette does not.
`

const percentOfOne = 100

type servePlan struct {
	stdio    bool
	schema   bool
	dir      string
	cassette string
}

func serveArgs(args []string) (servePlan, error) {
	plan := servePlan{cassette: os.Getenv(cassetteVariable)}
	valued := map[string]*string{"--dir": &plan.dir, "--cassette": &plan.cassette}
	for index := 0; index < len(args); index++ {
		target, takesValue := valued[args[index]]
		switch {
		case args[index] == "--stdio":
			plan.stdio = true
		case args[index] == "--schema":
			plan.schema = true
		case !takesValue:
			return plan, fmt.Errorf("unknown argument %q", args[index])
		case index+1 >= len(args):
			return plan, fmt.Errorf("%s wants a value", args[index])
		default:
			index++
			*target = args[index]
		}
	}
	if plan.stdio == plan.schema {
		return plan, errors.New("name exactly one of --stdio and --schema")
	}
	return plan, nil
}

func serveVerb(args []string, in io.Reader, out, errOut io.Writer) int {
	if slices.Contains(args, "--help") || slices.Contains(args, "-h") {
		_, _ = fmt.Fprint(out, serveUsage)
		return exitOK
	}
	plan, err := serveArgs(args)
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "tofu serve: %v\n\n%s", err, serveUsage)
		return exitUsage
	}
	if plan.schema {
		schema, err := host.Schema()
		if err != nil {
			return serveFail(errOut, err)
		}
		_, _ = fmt.Fprintln(out, string(schema))
		return exitOK
	}
	if plan.dir != "" {
		if err := os.Chdir(plan.dir); err != nil {
			return serveFail(errOut, err)
		}
	}
	dir, err := os.Getwd()
	if err != nil {
		return serveFail(errOut, err)
	}
	deck, err := readCassette(plan.cassette)
	if err != nil {
		return serveFail(errOut, err)
	}
	kept, err := quota.NewPoller(nil, time.Now, nil, recordQuotaReading)
	if err != nil {
		return serveFail(errOut, err)
	}
	open, polled, wires := openAppWire, serveQuota(kept), signedInWires
	if deck != nil {
		open, polled, wires = driveWire(deck), nil, func() []string { return append(runWires(), wireMeta) }
	}
	launch := launchOf(dir, sessionResume{}, true)
	engine := &appEngine{dir: dir, open: open, tabs: launch.tabs}
	live, troubles := host.New(host.Config{Dir: dir, Engine: engine, Shells: launch.registry, Check: cronChecker(dir)})
	for _, line := range append([]string{launch.note, "speaking " + host.Protocol + " for " + dir}, troubles...) {
		if line != "" {
			_, _ = fmt.Fprintln(errOut, "tofu serve: "+line)
		}
	}
	err = host.Serve(host.ServeConfig{Host: live, Dir: dir, In: in, Out: out, Shells: launch.registry, Carry: serveCarry, Verb: serveVerbs(errOut), Quota: polled,
		Sessions: serveSessions, Branch: serveBranch, Access: serveAccess, Wires: wires, Sources: wireSources(), Ledger: serveLedger, Compact: func() (host.Compaction, error) { return compactCarried(live) },
		Run: func(ctx context.Context, command string) (string, bool) {
			return shellCommand(ctx, dir, launch.registry, command)
		},
		Stale: func() bool { return catalogStale(time.Now()) }, Setup: serveSetup, SaveKey: serveKey, Logout: serveLogout})
	engine.warm.Close()
	live.Close()
	for _, warning := range turn.EndSession(context.Background(), dir, live.ID(), sessionEndExit) {
		_, _ = fmt.Fprintln(errOut, "tofu serve: "+warning)
	}
	leaveShells(launch.registry)
	if err != nil {
		return serveFail(errOut, err)
	}
	return exitOK
}

func serveFail(errOut io.Writer, err error) int {
	_, _ = fmt.Fprintf(errOut, "tofu serve: %v\n", err)
	return exitVerdict
}

func serveCarry(handle string) (host.Carry, error) {
	store, err := sessionstore.Open()
	if err != nil {
		return host.Carry{}, err
	}
	carry, err := resumeOf(store, handle)
	return carry.hosted(), err
}

func serveSessions(open string) (host.SessionList, error) {
	store, err := sessionstore.Open()
	if err != nil {
		return host.SessionList{}, err
	}
	report, err := sessionListing(store, sessionstore.DefaultSettings().Lifetime, time.Now())
	if err != nil {
		return host.SessionList{}, err
	}
	opened, _ := store.Identity(open)
	list := host.SessionList{Head: report.Head, Sessions: make([]host.SessionRow, len(report.Sessions))}
	for index, row := range report.Sessions {
		list.Sessions[index] = host.SessionRow{ID: row.ID, Name: row.Name, Handle: row.Handle, Family: row.Family, Generation: row.Generation, Generations: row.Generations,
			At: row.At, LastAt: row.LastAt, Task: row.Task, Turns: row.Turns, SubAgents: row.Agents, Wire: row.Wire, Model: row.Model, CostUSD: row.CostUSD,
			EndedAt: row.EndedAt, EndReason: string(row.EndReason), Outcome: row.Outcome, Expired: row.Expired, Open: row.ID == open || opened.Family != "" && row.Family == opened.Family}
		var held sessionstore.BusyError
		if errors.As(store.Busy(row.ID), &held) {
			list.Sessions[index].HeldBy = &host.SessionHolder{PID: held.PID, Since: held.Since}
		}
		list.Sessions[index].Kind = host.KindMain
		if side, found, _ := store.Side(row.ID); found {
			list.Sessions[index].Kind, list.Sessions[index].Owns, list.Sessions[index].Preset = host.KindSide, side.Owns, side.Preset
			list.Sessions[index].Parent = &host.SessionParent{Session: side.BranchedFrom.Session, Event: side.BranchedFrom.Event}
		}
	}
	return list, nil
}

func serveBranch(p host.SessionBranchParams) (host.SessionBranchResult, error) {
	store, err := sessionstore.Open()
	if err != nil {
		return host.SessionBranchResult{}, err
	}
	report, err := sessionBranch(store, sessionBranchAsk{handle: p.Session, preset: p.Preset, owns: strings.Join(p.Owns, ","), seed: cmp.Or(p.Seed, string(turn.SeedSummary)), name: p.Name, side: true})
	if err != nil {
		return host.SessionBranchResult{}, err
	}
	return host.SessionBranchResult{Session: report.Session, Handle: report.Handle, Parent: host.SessionParent{Session: report.Parent.Session, Event: report.Parent.Event},
		Owns: report.Owns, Preset: report.Preset, Carried: report.Carried}, nil
}

func serveAccess(p host.SessionAccessParams) (host.SessionAccess, error) {
	store, err := sessionstore.Open()
	if err != nil {
		return host.SessionAccess{}, err
	}
	side, err := sessionHeader(store, p.Session)
	if err == nil {
		err = store.Busy(side.ID)
	}
	if err != nil {
		return host.SessionAccess{}, err
	}
	access, err := turn.AccessOf(cmp.Or(p.Preset, turn.PresetRead))
	switch {
	case p.Preset != "" && len(p.Owns) > 0:
		err = errors.New("name preset or owns, not both")
	case len(p.Owns) > 0:
		access, err = turn.OwnsAccess(p.Owns)
	case err == nil && access.Preset == "":
		err = fmt.Errorf("preset %q is none of %s, %s and %s", p.Preset, turn.PresetRead, turn.PresetNotes, turn.PresetFiles)
	}
	if err != nil {
		return host.SessionAccess{}, err
	}
	header, err := store.SetAccess(side.ID, access.Owns, access.Preset)
	if err != nil {
		return host.SessionAccess{}, err
	}
	return host.SessionAccess{Session: header.ID, Owns: append([]string{}, header.Owns...), Preset: header.Preset}, nil
}

func serveLedger(p host.LedgerParams) (host.LedgerReport, error) {
	var args []string
	if p.ID != "" {
		args = append(args, p.ID)
	}
	if p.ID == "" || p.Last != 0 {
		args = append(args, "--last", strconv.Itoa(max(p.Last, 1)))
	}
	if p.Point != "" {
		args = append(args, "--point", p.Point)
	}
	opts, err := parseWhyArgs(args)
	if err != nil {
		return host.LedgerReport{}, &host.Refusal{Code: host.CodeBadParams, Message: err.Error()}
	}
	found, err := whyFind(opts)
	if err != nil {
		return host.LedgerReport{}, err
	}
	report, err := found.report()
	if err != nil {
		return host.LedgerReport{}, err
	}
	typed := host.LedgerReport{Rows: make([]host.LedgerRow, len(report.Rows))}
	if call := report.Call; call != nil {
		typed.Call = &host.LedgerCall{Session: call.Session, Step: call.Step, Attempt: call.Attempt, Call: call.Call}
	}
	for index, row := range report.Rows {
		typed.Rows[index] = host.LedgerRow{Row: row.Row, Chain: row.Chain, BlockedBy: row.BlockedBy, Precedents: row.Precedents}
	}
	return typed, nil
}

func serveSetup() []host.Requirement {
	var steps []host.Requirement
	for _, needed := range appRequirements() {
		step := host.Requirement{Step: needed.Step, What: needed.What, Fix: needed.Fix, Done: needed.Done}
		for _, choice := range needed.Choices {
			step.Choices = append(step.Choices, host.RequirementChoice{Label: choice.Label, Key: choice.Key})
		}
		steps = append(steps, step)
	}
	return steps
}

func serveKey(provider, key string) (string, error) {
	for _, stored := range keyStatuses(func(string) string { return "" }) {
		if stored.Provider == provider {
			return storeKeyFor(context.Background(), stored.Variable, key)
		}
	}
	return "", &host.Refusal{Code: host.CodeBadParams, Message: "tofu stores no key for " + strconv.Quote(provider)}
}

func serveLogout(p host.LogoutParams) (string, error) {
	args := []string{p.Role, p.Provider}
	if p.Number != 0 {
		args = append(args, strconv.Itoa(p.Number))
	}
	receipt, refusal := logout(args)
	if refusal != nil {
		return "", &host.Refusal{Code: host.CodeRefused, Message: strings.TrimSpace(refusal.problem.What + ": " + refusal.problem.Hint)}
	}
	return receipt.text, nil
}

func wireSources() map[string]string {
	return map[string]string{wireSubscription: string(models.ClaudeSub), wireCodex: string(models.CodexSub), wireKey: string(models.OpenRouter), wireMeta: string(models.Meta)}
}

func signedInWires() []string {
	var names []string
	for _, wire := range appWires() {
		names = append(names, wire.Name)
	}
	return names
}

func serveVerbs(errOut io.Writer) func([]string) (host.VerbResult, error) {
	return func(args []string) (host.VerbResult, error) {
		var out bytes.Buffer
		run(append(slices.Clone(args), jsonFlag), strings.NewReader(""), &out, errOut)
		var result host.VerbResult
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			return result, fmt.Errorf("tofu %s printed no report: %w", strings.Join(args, " "), err)
		}
		return result, nil
	}
}

func serveQuota(kept *quota.Poller) func() []host.QuotaWindow {
	return func() []host.QuotaWindow {
		var windows []host.QuotaWindow
		for _, read := range quotaFrames(pollCredentials(context.Background(), time.Now, kept)) {
			window := host.QuotaWindow{Account: read.Account, Window: read.Label, Percent: read.Fraction * percentOfOne, Reported: read.Reported}
			if !read.ResetsAt.IsZero() {
				window.ResetsAt = &read.ResetsAt
			}
			windows = append(windows, window)
		}
		return windows
	}
}
