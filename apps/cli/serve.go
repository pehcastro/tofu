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
	"sync"
	"time"

	"tofu/apps/cli/output"
	loginverb "tofu/apps/cli/verbs/login"
	shellsverb "tofu/apps/cli/verbs/shells"
	statusverb "tofu/apps/cli/verbs/status"
	"tofu/apps/cli/wiring"
	"tofu/internal/agent/loop"
	"tofu/internal/host"
	"tofu/internal/models/llm/catalog"
	"tofu/internal/models/llm/quota"
	"tofu/internal/models/transport"
	platformproject "tofu/internal/platform/project"
	sessionstore "tofu/internal/store/session"
	settingspkg "tofu/internal/store/settings"
)

const serveUsage = `usage: tofu serve --stdio [--dir PATH] [--cassette PATH] [--done-review off|cheap|screen|typed]
       tofu serve --schema

serves one project to a frontend over standard input and output: JSON-RPC 2.0,
one object a line, in the protocol tofu.host/1. Standard error carries logs and
nothing else. Closing standard input stops every running turn and ends tofu.

--schema prints the JSON Schema of every line tofu writes, with the lines it
reads under $defs.clientMessage.

--cassette PATH answers every model call from a recorded cassette, as tofu
drive does, so a frontend is built and tested with no model and no network.
TOFU_DRIVE_CASSETTE names it when --cassette does not. A cassette run reads
no quota, unless TOFU_CLAUDE_USAGE_URL or TOFU_CODEX_USAGE_URL names a
stand-in usage endpoint, which it then polls for quota.updated and
account.state.

--done-review off|cheap|screen|typed picks how a sub-agent's hand-back is
reviewed before the lead reads it, as tofu drive does. TOFU_SERVE_DONE_REVIEW
names it when --done-review does not, and carries it to the tofu host the
session runs in. With neither, the doneReview setting decides.
`

const (
	percentOfOne       = 100
	doneReviewVariable = "TOFU_SERVE_DONE_REVIEW"
)

type servePlan struct {
	stdio    bool
	schema   bool
	dir      string
	cassette string
	doneArm  string
}

func serveArgs(args []string) (servePlan, error) {
	plan := servePlan{cassette: os.Getenv(cassetteVariable), doneArm: os.Getenv(doneReviewVariable)}
	valued := map[string]*string{"--dir": &plan.dir, "--cassette": &plan.cassette, "--done-review": &plan.doneArm}
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
	return plan, armIsKnown("--done-review", plan.doneArm, settingspkg.DoneReviews())
}

func serveVerb(args []string, in io.Reader, out, errOut io.Writer) int {
	if slices.Contains(args, "--help") || slices.Contains(args, "-h") {
		_, _ = fmt.Fprint(out, serveUsage)
		return output.ExitOK
	}
	plan, err := serveArgs(args)
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "tofu serve: %v\n\n%s", err, serveUsage)
		return output.ExitUsage
	}
	if plan.schema {
		schema, err := host.Schema()
		if err != nil {
			return serveFail(errOut, err)
		}
		_, _ = fmt.Fprintln(out, string(schema))
		return output.ExitOK
	}
	_ = os.Setenv(doneReviewVariable, plan.doneArm)
	if plan.dir != "" {
		if err := os.Chdir(plan.dir); err != nil {
			return serveFail(errOut, err)
		}
	}
	collectForALongLife()
	dir, err := os.Getwd()
	if err != nil {
		return serveFail(errOut, err)
	}
	deck, err := readCassette(plan.cassette)
	if err != nil {
		return serveFail(errOut, err)
	}
	clock, err := driveClock(deck)
	if err != nil {
		return serveFail(errOut, err)
	}
	kept, err := quota.NewPoller(nil, time.Now, nil, wiring.RecordQuotaReading)
	if err != nil {
		return serveFail(errOut, err)
	}
	polled, accounts := serveQuota(kept)
	open, wires := deckOpener(deck), signedInWires
	if deck != nil {
		_ = os.Setenv(cassetteVariable, plan.cassette)
		wires = func() []string { return append(wiring.RunWires(), wiring.WireMeta) }
		if os.Getenv(quota.ClaudeUsageURLVariable) == "" && os.Getenv(quota.CodexUsageURLVariable) == "" {
			polled = nil
		}
	}
	launch := launchOf(dir, sessionResume{}, true)
	say := func(line string) {
		if line != "" {
			_, _ = fmt.Fprintln(errOut, "tofu serve: "+line)
		}
	}
	say(launch.note)
	say("speaking " + host.Protocol + " for " + dir)
	places, err := platformproject.ResolvePlaces(".")
	if err != nil {
		return serveFail(errOut, err)
	}
	spawn := func() (*host.Host, func()) {
		engine := &appEngine{dir: dir, open: open, tabs: launch.tabs, places: places}
		live, troubles := host.New(host.Config{Dir: dir, Engine: engine, Shells: launch.registry, Check: shellsverb.CronChecker(dir), Joins: true, Now: clock})
		for _, trouble := range troubles {
			say(trouble)
		}
		return live, func() {
			live.Close()
			engine.warm.Close()
		}
	}
	live, release := spawn()
	recent := &sessionstore.Recent{}
	err = host.Serve(host.ServeConfig{Host: live, Release: release, Spawn: spawn, Dir: dir, In: in, Out: out, Shells: launch.registry, Carry: serveCarry(recent), Verb: serveVerbs(errOut, recent), Quota: polled, Accounts: accounts,
		Sessions: serveSessions, Branch: serveBranch, Access: serveAccess, Wires: wires, Sources: wireSources(), Ledger: serveLedger, Fork: (*host.Host).Fork,
		Run: func(ctx context.Context, command string) (string, bool) {
			return shellCommand(ctx, dir, launch.registry, command)
		},
		Stale: func() bool { return wiring.CatalogStale(time.Now()) }, Setup: serveSetup, SaveKey: serveKey, Logout: serveLogout})
	leaveShells(launch.registry)
	if err != nil {
		return serveFail(errOut, err)
	}
	return output.ExitOK
}

func serveFail(errOut io.Writer, err error) int {
	_, _ = fmt.Fprintf(errOut, "tofu serve: %v\n", err)
	return output.ExitVerdict
}

func serveCarry(recent *sessionstore.Recent) func(string) (host.Carry, error) {
	return func(handle string) (host.Carry, error) {
		store, err := sessionstore.Open()
		if err != nil {
			return host.Carry{}, err
		}
		store.Recalls(recent)
		carry, err := resumeOf(store, handle)
		return carry.hosted(), err
	}
}

func serveSessions(open string, wanted host.SessionListParams) (host.SessionList, error) {
	store, err := sessionstore.Open()
	if err != nil {
		return host.SessionList{}, err
	}
	store = store.ReadOnce()
	opened, _ := store.Identity(open)
	members := host.MembersOf(store.Project())
	list, matched := host.SessionList{Sessions: []host.SessionRow{}}, 0
	report, err := sessionListingOf(store, func(store *sessionstore.Store, handle string) (sessionRow, error) {
		header, err := sessionHeader(store, handle)
		if err != nil {
			return sessionRow{}, err
		}
		if header.Schema != 0 {
			return sessionHeadRow(store, header), nil
		}
		row, _, err := sessionSummary(store, handle)
		return row, err
	}, func(row sessionRow) bool {
		listed := host.SessionRow{ID: row.ID, Name: row.Name, Handle: row.Handle, Family: row.Family, Generation: row.Generation, Generations: row.Generations,
			At: row.At, LastAt: row.LastAt, Task: row.Task, Turns: row.Turns, SubAgents: row.Agents, Wire: row.Wire, Model: row.Model, CostUSD: row.CostUSD,
			EndedAt: row.EndedAt, EndReason: string(row.EndReason), Outcome: row.Outcome, Expired: row.Expired, Open: row.ID == open || opened.Family != "" && row.Family == opened.Family,
			Members: members[row.ID], Kind: host.KindMain}
		if row.Parent != "" {
			listed.Parent = &host.SessionParent{Session: row.Parent}
		}
		if side, found, _ := store.Side(row.ID); found {
			listed.Kind, listed.Owns, listed.Preset = host.KindSide, side.Owns, side.Preset
			listed.Parent = &host.SessionParent{Session: side.BranchedFrom.Session, Event: side.BranchedFrom.Event}
		}
		list.Sessions = append(list.Sessions, listed)
		if wanted.Wants(listed) {
			matched++
		}
		return wanted.Limit > 0 && matched == wanted.Limit
	})
	list.Head = report.Head
	return list, err
}

func serveBranch(p host.SessionBranchParams) (host.SessionBranchResult, error) {
	store, err := sessionstore.Open()
	if err != nil {
		return host.SessionBranchResult{}, err
	}
	ask := sessionBranchAsk{handle: p.Session, preset: p.Preset, owns: strings.Join(p.Owns, ","), seed: cmp.Or(p.Seed, string(loop.SeedSummary)), name: p.Name,
		model: p.Model, effort: string(p.Effort), side: true}
	if p.At != nil {
		ask.at = p.At.Item
	}
	report, err := sessionBranch(store, ask)
	if err != nil {
		return host.SessionBranchResult{}, err
	}
	return host.SessionBranchResult{Session: report.Session, Handle: report.Handle, Parent: host.SessionParent{Session: report.Parent.Session, Event: report.Parent.Event},
		Owns: report.Owns, Preset: report.Preset, Carried: report.Carried, Model: report.Model, Effort: report.Effort}, nil
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
	access, err := loop.AccessOf(cmp.Or(p.Preset, loop.PresetRead))
	switch {
	case p.Preset != "" && len(p.Owns) > 0:
		err = errors.New("name preset or owns, not both")
	case len(p.Owns) > 0:
		access, err = loop.OwnsAccess(p.Owns)
	case err == nil && access.Preset == "":
		err = fmt.Errorf("preset %q is none of %s, %s and %s", p.Preset, loop.PresetRead, loop.PresetNotes, loop.PresetFiles)
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
	opts, err := statusverb.ParseWhyArgs(args)
	switch {
	case err != nil:
	case p.ID != "" && p.Session != "":
		err = errors.New("session goes with last, not with an id")
	case p.ID != "" && p.Before != "":
		err = errors.New("before goes with last, not with an id")
	}
	opts.Before = p.Before
	if err != nil {
		return host.LedgerReport{}, &host.Refusal{Code: host.CodeBadParams, Message: err.Error()}
	}
	if p.Session != "" {
		if opts.Turns, opts.Since, err = host.SessionTurnIDs(".", p.Session); err != nil {
			return host.LedgerReport{}, err
		}
	}
	found, err := statusverb.WhyFind(opts)
	if err != nil {
		return host.LedgerReport{}, err
	}
	report, err := found.Report()
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
	held := wiring.HoldStore()
	defer held.Close()
	var steps []host.Requirement
	for _, needed := range held.Requirements() {
		step := host.Requirement{Step: needed.Step, What: needed.What, Fix: needed.Fix, Done: needed.Done}
		for _, choice := range needed.Choices {
			step.Choices = append(step.Choices, host.RequirementChoice{Label: choice.Label, Key: choice.Key})
		}
		steps = append(steps, step)
	}
	return steps
}

func serveKey(provider, key string) (string, error) {
	for _, stored := range wiring.KeyStatuses(func(string) string { return "" }) {
		if stored.Provider == provider {
			return loginverb.StoreKeyFor(context.Background(), stored.Variable, key)
		}
	}
	return "", &host.Refusal{Code: host.CodeBadParams, Message: "tofu stores no key for " + strconv.Quote(provider)}
}

func serveLogout(p host.LogoutParams) (string, error) {
	args := []string{p.Role, p.Provider}
	if p.Number != 0 {
		args = append(args, strconv.Itoa(p.Number))
	}
	receipt, refusal := loginverb.Logout(args)
	if refusal != nil {
		return "", &host.Refusal{Code: host.CodeRefused, Message: strings.TrimSpace(refusal.Problem.What + ": " + refusal.Problem.Hint)}
	}
	return receipt.Text, nil
}

func wireSources() map[string]string {
	return map[string]string{wiring.WireSubscription: string(catalog.ClaudeSub), wiring.WireCodex: string(catalog.CodexSub), wiring.WireKey: string(catalog.OpenRouter), wiring.WireMeta: string(catalog.Meta)}
}

func signedInWires() []string {
	held := wiring.HoldStore()
	defer held.Close()
	var names []string
	for _, wire := range held.Wires() {
		names = append(names, wire.Name)
	}
	return names
}

func serveVerbs(errOut io.Writer, recent *sessionstore.Recent) func([]string) (host.VerbResult, error) {
	return func(args []string) (host.VerbResult, error) {
		var out bytes.Buffer
		asked := append(slices.Clone(args), output.JSONFlag)
		if asked[0] == "session" {
			sessionVerbRecalling(recent, asked[1:], strings.NewReader(""), &out, errOut)
		} else {
			run(asked, strings.NewReader(""), &out, errOut)
		}
		var result host.VerbResult
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			return result, fmt.Errorf("tofu %s printed no report: %w", strings.Join(args, " "), err)
		}
		return result, nil
	}
}

func serveQuota(kept *quota.Poller) (func() []host.QuotaWindow, func() []host.AccountNow) {
	var mu sync.Mutex
	var last []wiring.PollResult
	windows := func() []host.QuotaWindow {
		results, _ := wiring.PollCredentials(context.Background(), time.Now, kept)
		mu.Lock()
		last = results
		mu.Unlock()
		var windows []host.QuotaWindow
		for _, result := range results {
			for _, read := range wiring.QuotaFrames([]wiring.PollResult{result}, nil) {
				window := host.QuotaWindow{Account: read.Account, Window: read.Label, Percent: read.Fraction * percentOfOne, Reported: read.Reported,
					Source: string(result.Report.Provider), AccountID: result.Row, Stale: read.Stale, ReadAt: read.ReadAt.UTC()}
				if !read.ResetsAt.IsZero() {
					window.ResetsAt = &read.ResetsAt
				}
				windows = append(windows, window)
			}
		}
		return windows
	}
	accounts := func() []host.AccountNow {
		mu.Lock()
		defer mu.Unlock()
		var held []host.AccountNow
		for _, result := range last {
			now := host.AccountNow{Source: string(result.Report.Provider), AccountID: result.Row, RetryAt: result.Report.RetryAt.UTC()}
			switch condition := quota.Diagnose(result.Report, result.Err); {
			case transport.KindOf(result.Err) == transport.KindRateLimit:
				now.State = host.ConditionRateLimited
			case condition == quota.ConditionWindowSpent:
				now.State = host.ConditionSpent
			case condition == quota.ConditionServing:
				now.State = host.ConditionServing
			default:
				continue
			}
			held = append(held, now)
		}
		return held
	}
	return windows, accounts
}
