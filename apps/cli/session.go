package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"tofu/apps/cli/output"
	cli "tofu/apps/cli/page"
	"tofu/apps/cli/wiring"
	"tofu/internal/agent/cron"
	"tofu/internal/agent/loop"
	"tofu/internal/cli/render"
	"tofu/internal/host"
	"tofu/internal/judge/ledger"
	"tofu/internal/models/llm/core"
	"tofu/internal/models/llm/wire/anthropic"
	"tofu/internal/platform/safe"
	"tofu/internal/store/session"
	settingspkg "tofu/internal/store/settings"
	"tofu/internal/sys"
	"tofu/internal/ui/tui/panels/trace"
)

const (
	sessionSubcommands = "tofu session <name|id|head>, or tofu session list [--all], or tofu session clean [--dry-run], or tofu session set <name|id|head> --model source/model, or tofu session work <name|id|head> --ticket ID, or tofu session info|trace|transcript|drain|reads|resume|rename|find|branch <name|id|head> [--json], or tofu session request <name|id|head> [<request id>|last|--has kind/source] [--json]"
	sessionSetUsage    = "tofu session set <name|id|head> --model source/model [--json]"
	sessionFresh       = "no session recorded here"
	sessionDay         = 24 * time.Hour
	sessionWeek        = 7 * sessionDay
	sessionClock       = "Mon 15:04"
	sessionDate        = "2 Jan 15:04"
)

var failureShaped = sys.LazyRegexp(`(?i)\b(fail|failed|failure|errors?|fatal|panic|exception|denied|not found|no such file|undefined)\b`)

type sessionSkip = host.SessionSkip

type sessionRow struct {
	host.SessionInfo
	lastAt time.Time
	tasks  []string
	sideOf string
	fork   *contextForkReport
}

type sessionListReport struct {
	Head        string           `json:"head,omitempty"`
	HeadDerived bool             `json:"head_derived,omitempty"`
	Lifetime    session.Lifetime `json:"lifetime"`
	Expired     int              `json:"expired"`
	Sessions    []sessionRow     `json:"sessions"`
	Skipped     []sessionSkip    `json:"skipped,omitempty"`
}

type sessionReadsReport struct {
	Session    string         `json:"session"`
	Name       string         `json:"name,omitempty"`
	Reads      []session.Read `json:"reads"`
	Unrecorded int            `json:"unrecorded"`
}

type sessionResume struct {
	Session     string             `json:"session,omitempty"`
	Name        string             `json:"name,omitempty"`
	Handle      string             `json:"handle,omitempty"`
	Task        string             `json:"task,omitempty"`
	Outcome     string             `json:"outcome,omitempty"`
	Steps       int                `json:"steps"`
	Carried     int                `json:"carried_messages"`
	CarriedFrom *host.CarriedFrom  `json:"carried_from,omitempty"`
	HeadDerived bool               `json:"head_derived,omitempty"`
	Fresh       string             `json:"fresh,omitempty"`
	Busy        *session.BusyError `json:"busy,omitempty"`

	messages []core.Message
	tasks    []string
	read     *session.Store
	keys     *sys.KeyRedactor
}

func sessionOperands(subcommand string) (string, int, bool) {
	switch subcommand {
	case "list":
		return "", 0, true
	case "info", "reads", "resume":
		return "<name|id|head>", 1, true
	case "rename":
		return "<name|id|head> <new name>", 2, true
	case "request":
		return "<name|id|head> [<request id>|last|--has kind/source]", 2, true
	}
	return "", 0, false
}

func sessionVerb(args []string, in io.Reader, out, errOut io.Writer) int {
	return sessionVerbRecalling(nil, args, in, out, errOut)
}

func sessionVerbRecalling(recent *session.Recent, args []string, in io.Reader, out, errOut io.Writer) int {
	all := len(args) > 0 && args[0] == "list" && slices.Contains(args, "--all")
	args = slices.DeleteFunc(slices.Clone(args), func(arg string) bool { return all && arg == "--all" })
	has, asksHas := "", false
	if at := slices.Index(args, hasFlag); at > 0 && args[0] == "request" {
		end := min(at+2, len(args))
		has, asksHas = strings.Join(args[at+1:end], ""), true
		args = slices.Delete(args, at, end)
	}
	handles, _, err := output.VerbArgs(args[min(1, len(args)):])
	o := output.Verb{Name: "session", UsageLine: sessionSubcommands, AsJSON: output.JSONAsked(args), Out: out, ErrOut: errOut}
	if len(output.WithoutJSON(args)) == 0 {
		return o.Usage(errors.New("no subcommand"))
	}
	now := time.Now()
	if args[0] == "branch" {
		o.Name, o.UsageLine = "session branch", sessionBranchUsage
		ask, err := sessionBranchArgs(args[1:])
		if err != nil {
			return o.Usage(err)
		}
		return sessionReport(o, func(store *session.Store) (sessionBranchReport, error) { return sessionBranch(store, ask) }, sessionBranchLines)
	}
	if args[0] == "trace" {
		return sessionTraceVerb(o, args[1:], now)
	}
	if args[0] == "work" {
		return sessionWorkVerb(o, args[1:])
	}
	if args[0] == "transcript" {
		return sessionTranscriptVerb(o, args[1:])
	}
	if args[0] == "drain" {
		return sessionDrainVerb(o, args[1:])
	}
	if args[0] == "clean" {
		return sessionCleanVerb(o, args[1:], now)
	}
	if args[0] == "set" {
		o.Name, o.UsageLine = "session set", sessionSetUsage
		handle, slug, err := sessionSetArgs(args[1:])
		if err != nil {
			return o.Usage(err)
		}
		return sessionReport(o, func(store *session.Store) (sessionSetReport, error) { return sessionSetModel(store, handle, slug) }, sessionSetLines)
	}
	if args[0] == "find" {
		o.Name, o.UsageLine = "session find", sessionFindUsage
		handle, query, err := sessionFindArgs(args[1:], now)
		if err != nil {
			return o.Usage(err)
		}
		return sessionReport(o, func(store *session.Store) (sessionFindReport, error) { return sessionFind(store, handle, query) },
			func(page cli.Page, report sessionFindReport) []string { return sessionFindLines(page, report, now) })
	}
	operands, wanted, known := sessionOperands(args[0])
	if !known {
		o.UsageLine = "tofu session <name|id|head>[.N] [--family] [--json]"
		whole := slices.Contains(args, familyFlag)
		if handles, _, err = output.VerbArgs(slices.DeleteFunc(slices.Clone(args), func(arg string) bool { return arg == familyFlag })); err == nil && len(handles) != 1 {
			err = fmt.Errorf("%d operands, want <name|id>", len(handles))
		}
		if err != nil {
			return o.Usage(err)
		}
		base, number, numbered := generationAsked(handles[0])
		if whole || !numbered {
			return sessionReport(o, func(store *session.Store) (sessionFamilyReport, error) { return sessionFamilyOf(store, handles[0]) },
				func(page cli.Page, report sessionFamilyReport) []string { return sessionFamilyLines(page, report, now) })
		}
		store, err := session.Open()
		if err != nil {
			return o.Fail(err)
		}
		family, _, err := sessionFamily(store, base)
		if err != nil {
			return o.Fail(err)
		}
		if number < 1 || number > len(family.Generations) {
			return o.Refuse(output.ExitUsage, cli.Problem{What: fmt.Sprintf("%s has no generation .%d; the last is %s", base, number, family.Handle()),
				Hint: "tofu session " + family.Handle()})
		}
		args[0], operands, wanted = "info", "<name|id|head>", 1
	}
	o.Name, o.UsageLine = "session "+args[0], strings.TrimSpace("tofu session "+args[0]+" "+operands)+" [--json]"
	listsRequests := args[0] == "request" && len(handles) == 1
	if err == nil && len(handles) != wanted && !listsRequests {
		err = fmt.Errorf("%d operands, want %s", len(handles), cmp.Or(operands, "none"))
	}
	if err == nil && asksHas && !listsRequests {
		err = errors.New(hasFlag + " lists every request, so it takes no request id")
	}
	if err != nil {
		return o.Usage(err)
	}
	if asksHas && !slices.Contains(requestOrigins(), has) {
		return o.Refuse(output.ExitUsage, cli.Problem{What: fmt.Sprintf("no origin %q", has), Hint: "known origins: " + strings.Join(requestOrigins(), ", ")})
	}
	store, err := session.Open()
	if err != nil {
		return o.Fail(err)
	}
	store.Recalls(recent)
	lifetime := sessionLifetime(".", o.ErrOut)
	switch args[0] {
	case "list":
		report, err := sessionListing(store)
		if err != nil {
			return o.Fail(err)
		}
		report.Lifetime = lifetime
		for i := range report.Sessions {
			row := &report.Sessions[i]
			if row.Expired = lifetime.Expired(row.lastAt, now); row.Expired {
				report.Expired++
			}
		}
		report.Sessions = slices.DeleteFunc(report.Sessions, func(row sessionRow) bool { return !all && row.sideOf != "" })
		return o.Done(true, report, func(page cli.Page) []string { return sessionListLines(page, report, now) })
	case "info":
		row, err := sessionDetail(store, handles[0])
		if err != nil {
			return o.Fail(err)
		}
		if head, headErr := store.Head(); headErr == nil {
			row.Head = head.ID == row.ID
		}
		row.Expired = lifetime.Expired(row.lastAt, now)
		return o.Done(true, row, func(page cli.Page) []string { return sessionInfoLines(page, row, now, store) })
	case "reads":
		report, err := sessionReads(store, handles[0])
		if err != nil {
			return o.Fail(err)
		}
		return o.Done(true, report, func(page cli.Page) []string { return sessionReadsLines(page, report) })
	case "request":
		if asksHas {
			report, err := sessionRequestsHaving(store, handles[0], has)
			if err != nil {
				return o.Fail(err)
			}
			return o.Done(true, report, func(page cli.Page) []string { return sessionRequestsHavingLines(page, report) })
		}
		if listsRequests {
			report, err := sessionRequestList(store, handles[0])
			if err != nil {
				return o.Fail(err)
			}
			return o.Done(true, report, func(page cli.Page) []string { return sessionRequestListLines(page, report) })
		}
		report, err := sessionRequest(store, handles[0], handles[1])
		if err != nil {
			return o.Fail(err)
		}
		return o.Done(true, report, func(page cli.Page) []string { return sessionRequestLines(page, report) })
	case "rename":
		row, err := sessionRenamed(store, handles[0], handles[1])
		if err != nil {
			return o.Fail(err)
		}
		return o.Done(true, row, func(page cli.Page) []string {
			return append([]string{page.Glyph(cli.Changed) + " renamed " + row.ID + " to " + row.Name},
				cli.Indent(page.Hint("tofu session info "+row.Name))...)
		})
	}
	carry, err := resumeOf(store, handles[0])
	if err != nil {
		return o.Fail(err)
	}
	return startResumed(o, carry, in)
}

func continueVerb(args []string, in io.Reader, out, errOut io.Writer) int {
	handles, asJSON, err := output.VerbArgs(args)
	o := output.Verb{Name: "--continue", UsageLine: "tofu --continue [--json], or tofu session resume <name|id|head>", AsJSON: asJSON, Out: out, ErrOut: errOut}
	if err == nil && len(handles) != 0 {
		err = fmt.Errorf("takes the head, not %q", handles[0])
	}
	if err != nil {
		return o.Usage(err)
	}
	store, err := session.Open()
	if err != nil {
		return o.Fail(err)
	}
	return startResumed(o, continueCarry(store), in)
}

func startResumed(o output.Verb, carry sessionResume, in io.Reader) int {
	if o.AsJSON {
		return o.Done(carry.Busy == nil, carry, func(page cli.Page) []string { return busyResumeLines(page, carry) })
	}
	return appVerb(in, o.Out, o.ErrOut, carry)
}

func continueCarry(store *session.Store) sessionResume {
	head, err := store.Head()
	if err != nil {
		return sessionResume{Fresh: sessionFresh}
	}
	carry, err := resumeOf(store, head.ID)
	if err != nil {
		return sessionResume{Fresh: "the head " + head.ID + " does not read: " + err.Error()}
	}
	carry.HeadDerived = head.Derived
	return carry
}

func resumeOf(store *session.Store, id string) (sessionResume, error) {
	var keys sys.KeyRedactor
	var keysErr error
	var loaded sync.WaitGroup
	loaded.Go(func() {
		keysErr = sys.Crashed(".", safe.Call(nil, "key redactor load", func() error {
			keys = sys.LoadKeyRedactor()
			return nil
		}))
	})
	defer loaded.Wait()
	store = store.ReadOnce()
	row, recorded, err := sessionSummary(store, id)
	if err != nil {
		return sessionResume{}, err
	}
	said, steps, err := store.ReadSaid(row.ID, recorded)
	if err != nil {
		return sessionResume{}, err
	}
	messages, err := loop.ConversationOf(said)
	if err != nil {
		return sessionResume{}, err
	}
	carry := sessionResume{
		Session:     row.ID,
		Name:        row.Name,
		Handle:      row.Handle,
		Task:        row.Task,
		Outcome:     row.Outcome,
		Steps:       steps,
		Carried:     len(messages),
		CarriedFrom: row.CarriedFrom,
		messages:    messages,
		tasks:       row.tasks,
		read:        store,
		keys:        &keys,
	}
	var busy session.BusyError
	switch err := store.Busy(row.ID); {
	case errors.As(err, &busy):
		carry.Busy = &busy
	case err != nil:
		return sessionResume{}, err
	}
	if loaded.Wait(); keysErr != nil {
		return sessionResume{}, keysErr
	}
	return carry, nil
}

func (carry sessionResume) hosted() host.Carry {
	return host.Carry{Session: carry.Session, Name: carry.Name, Messages: carry.messages, Tasks: carry.tasks, Store: carry.read, Keys: carry.keys}
}

func sessionRenamed(store *session.Store, handle, to string) (sessionRow, error) {
	row, err := sessionDetail(store, handle)
	if err != nil {
		return sessionRow{}, err
	}
	header, err := store.SetName(row.ID, to)
	if err != nil {
		return sessionRow{}, err
	}
	row.Name, row.Handle = *header.Name, handleOf(store, row.ID)
	return row, nil
}

type sessionSetReport struct {
	sessionRow
	Live bool `json:"live"`
}

func sessionSetArgs(args []string) (string, string, error) {
	slug := ""
	if at := slices.Index(args, "--model"); at >= 0 && at+1 < len(args) {
		slug, args = args[at+1], slices.Delete(slices.Clone(args), at, at+2)
	}
	handles, _, err := output.VerbArgs(args)
	switch {
	case err != nil:
		return "", "", err
	case slug == "":
		return "", "", errors.New("--model source/model names the model, as tofu models list prints it")
	case len(handles) != 1:
		return "", "", fmt.Errorf("%d operands, want <name|id>", len(handles))
	}
	return handles[0], slug, nil
}

func sessionSetModel(store *session.Store, handle, slug string) (sessionSetReport, error) {
	header, err := sessionHeader(store, handle)
	if err != nil {
		return sessionSetReport{}, err
	}
	library, err := wiring.ModelLibrary(".")
	if err != nil {
		return sessionSetReport{}, err
	}
	model, err := library.Select(slug)
	if err != nil {
		return sessionSetReport{}, err
	}
	pick := host.Pick{Wire: library.WireOf(model), Model: model.Slug()}
	if _, err := store.SetModel(header.ID, pick.Wire, pick.Model); err != nil {
		return sessionSetReport{}, err
	}
	live, err := host.SettleHeld(cmp.Or(store.Project(), "."), header.ID, pick)
	if err != nil {
		return sessionSetReport{}, fmt.Errorf("%s now names %s, and the tofu host holding it did not take the change: %w", header.ID, pick.Model, err)
	}
	row, err := sessionDetail(store, header.ID)
	return sessionSetReport{sessionRow: row, Live: live}, err
}

func sessionSetLines(page cli.Page, report sessionSetReport) []string {
	when := "it runs on it from its next resume"
	if report.Live {
		when = "the tofu host holding it runs its next turn on it"
	}
	return append([]string{page.Glyph(cli.Changed) + " " + report.Handle + " is set to " + report.Model + ", and " + when},
		cli.Indent(page.Hint("tofu session info "+report.Handle))...)
}

func sessionHeader(store *session.Store, handle string) (session.Header, error) {
	matched, err := store.Resolve(handle)
	if errors.Is(err, fs.ErrNotExist) && handle == session.HeadHandle {
		return session.Header{}, output.ProblemError{What: sessionFresh, Hint: "tofu session list"}
	}
	if errors.Is(err, fs.ErrNotExist) {
		return session.Header{}, output.ProblemError{What: "no session " + handle + " here", Hint: "tofu session list"}
	}
	if err != nil {
		return session.Header{}, err
	}
	if len(matched) > 1 {
		ids := make([]string, len(matched))
		for i, header := range matched {
			ids[i] = header.ID + " (" + sessionWhen(header.At, time.Now()) + ")"
		}
		return session.Header{}, output.ProblemError{What: strconv.Itoa(len(matched)) + " sessions are called " + handle + ": " + strings.Join(ids, ", "), Hint: "name one by id"}
	}
	return matched[0], nil
}

func sessionReads(store *session.Store, handle string) (sessionReadsReport, error) {
	header, err := sessionHeader(store, handle)
	if err != nil {
		return sessionReadsReport{}, err
	}
	reads, err := store.ReadsOf(header.ID)
	if err != nil {
		return sessionReadsReport{}, err
	}
	report := sessionReadsReport{Session: header.ID, Reads: append([]session.Read{}, reads.Reads...), Unrecorded: reads.Unrecorded}
	if header.Name != nil {
		report.Name = *header.Name
	}
	return report, nil
}

func sessionDetail(store *session.Store, handle string) (sessionRow, error) {
	row, recorded, err := sessionSummary(store, handle)
	if err != nil {
		return sessionRow{}, err
	}
	events, err := store.Part(recorded, "")
	if err != nil {
		return sessionRow{}, err
	}
	reading, err := session.ReadEvents(events)
	if err != nil {
		return sessionRow{}, err
	}
	messages, err := loop.ConversationOf(reading)
	if err != nil {
		return sessionRow{}, err
	}
	for _, event := range events {
		switch event.Kind {
		case session.EventStep:
			row.Steps++
		case session.EventRead:
			row.Reads++
		}
	}
	row.Carried = len(messages)
	if row.fork != nil {
		steps, err := stepRowsOf(reading)
		if err != nil {
			return sessionRow{}, err
		}
		for _, step := range steps {
			if step.Fork != nil {
				row.fork.Kind = string(step.Fork.Kind)
			}
		}
		row.ForkIntoKind = row.fork.Kind
	}
	return row, nil
}

func sessionSummary(store *session.Store, handle string) (sessionRow, []session.Event, error) {
	header, err := sessionHeader(store, handle)
	if err != nil {
		return sessionRow{}, nil, err
	}
	events, err := store.Events(header.ID)
	if err != nil {
		return sessionRow{}, nil, err
	}
	if err := loop.CheckToolOutcomes(events); err != nil {
		return sessionRow{}, nil, err
	}
	row := sessionHeadRow(store, header)
	row.fork = headerFork(store, header)
	for _, event := range events {
		var outcome struct {
			Task string `json:"task"`
		}
		ended := event.Kind == session.EventOutcome || event.Kind == session.EventTurnEnd
		if event.Agent == "" && ended && json.Unmarshal(event.Body, &outcome) == nil {
			row.tasks = append(row.tasks, outcome.Task)
		}
	}
	row.Turns, row.lastAt = max(header.Turns, len(row.tasks)), sessionActivity(store, header, events)
	if header.CarriedFrom != nil {
		part, err := store.Part(events, header.ID)
		if err != nil {
			return sessionRow{}, nil, err
		}
		given, err := loop.ConversationFrom(part)
		if err != nil {
			return sessionRow{}, nil, err
		}
		row.CarriedFrom = &host.CarriedFrom{Session: header.CarriedFrom.Session, Handle: handleOf(store, header.CarriedFrom.Session), Messages: len(given)}
	}
	return row, events, nil
}

func sessionHeadRow(store *session.Store, header session.Header) sessionRow {
	row := sessionRow{SessionInfo: host.SessionInfo{
		ID:               header.ID,
		At:               header.At,
		LastAt:           host.LastAt(store, header.ID),
		Task:             header.Task,
		Turns:            header.Turns,
		Agents:           len(header.Agents),
		Outcome:          header.Outcome,
		Error:            header.Error,
		Wire:             header.Wire,
		Model:            header.Model,
		Parent:           header.Parent,
		Root:             header.Root,
		ForkedInto:       header.ForkedInto,
		ForkKind:         header.ForkKind,
		ContextCeiling:   header.ContextCeiling,
		ContextTarget:    header.ContextTarget,
		AutoCompaction:   header.AutoCompaction,
		ForkTokensBefore: header.ForkTokensBefore,
		ForkTokensAfter:  header.ForkTokensAfter,
		CostUSD:          header.CostUSD,
		EndedAt:          header.EndedAt,
		EndReason:        header.EndReason,
	}}
	if header.Name != nil {
		row.Name = *header.Name
	}
	row.Handle = sessionShortID(header.ID)
	if side, found, _ := store.Side(header.ID); found {
		row.sideOf = handleOf(store, side.BranchedFrom.Session)
	}
	if identity, err := store.Identity(header.ID); err == nil {
		row.Handle, row.Family, row.Generation = identity.Handle(), identity.Family, identity.Generation
	}
	return row
}

func sessionListing(store *session.Store) (sessionListReport, error) {
	return sessionListingOf(store, sessionDetail, func(sessionRow) bool { return false })
}

func sessionListingOf(store *session.Store, rowOf func(*session.Store, string) (sessionRow, error), enough func(sessionRow) bool) (sessionListReport, error) {
	listing, err := store.Listing()
	if err != nil {
		return sessionListReport{}, err
	}
	report := sessionListReport{Sessions: []sessionRow{}}
	if head, err := store.Head(); err == nil {
		report.Head, report.HeadDerived = head.ID, head.Derived
	}
	for _, skip := range listing.Skipped {
		report.Skipped = append(report.Skipped, sessionSkip{Session: skip.ID, Reason: skip.Reason.Error()})
	}
	for _, family := range listing.Families() {
		row, err := rowOf(store, family.Session)
		if err != nil {
			report.Skipped = append(report.Skipped, sessionSkip{Session: family.Session, Reason: err.Error()})
			continue
		}
		row.Head, row.Generations = slices.ContainsFunc(family.Generations, func(header session.Header) bool { return header.ID == report.Head }), len(family.Generations)
		report.Sessions = append(report.Sessions, row)
		if enough(row) {
			break
		}
	}
	return report, nil
}

const (
	sessionStaleSetting = "sessionStaleDays"
	sessionGoneSuffix   = ".gone"
	sessionActive       = "active"
)

type sessionSweepReport struct {
	Lifetime session.Lifetime `json:"lifetime"`
	DryRun   bool             `json:"dry_run,omitempty"`
	Deleted  []string         `json:"deleted"`
	Kept     []sessionSkip    `json:"kept,omitempty"`
	Failed   []sessionSkip    `json:"failed,omitempty"`
}

func sessionLifetime(dir string, errOut io.Writer) session.Lifetime {
	return session.Lifetime(settingspkg.IntSaying(dir, sessionStaleSetting, func(line string) { _, _ = fmt.Fprintln(errOut, "tofu: "+line) }))
}

func sweepSessions(dir string, say func(string)) {
	store, err := session.OpenIn(dir)
	report := sessionSweepReport{}
	if err == nil {
		report, err = sessionSweep(store, session.Lifetime(settingspkg.IntSaying(dir, sessionStaleSetting, say)), time.Now(), false)
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		say("the stale session sweep stopped: " + err.Error())
	}
	if len(report.Deleted) > 0 {
		say("deleted " + output.Plural(len(report.Deleted), "session") + " idle past " + report.Lifetime.String() + ": " + strings.Join(report.Deleted, ", "))
	}
	for _, failed := range report.Failed {
		say("the stale session " + failed.Session + " was not deleted: " + failed.Reason)
	}
}

func sessionCleanVerb(o output.Verb, args []string, now time.Time) int {
	o.Name, o.UsageLine = "session clean", "tofu session clean [--dry-run] [--json]"
	dryRun := slices.Contains(args, "--dry-run")
	if rest := slices.DeleteFunc(output.WithoutJSON(args), func(arg string) bool { return arg == "--dry-run" }); len(rest) > 0 {
		return o.Usage(fmt.Errorf("unknown argument %q", rest[0]))
	}
	store, err := session.Open()
	if err != nil {
		return o.Fail(err)
	}
	report, err := sessionSweep(store, sessionLifetime(".", o.ErrOut), now, dryRun)
	if err != nil {
		return o.Fail(err)
	}
	return o.Done(len(report.Failed) == 0, report, func(page cli.Page) []string { return sessionCleanLines(page, report) })
}

func sessionSweep(store *session.Store, lifetime session.Lifetime, now time.Time, dryRun bool) (sessionSweepReport, error) {
	report := sessionSweepReport{Lifetime: lifetime, DryRun: dryRun, Deleted: []string{}}
	if lifetime == session.LifetimeNever {
		return report, nil
	}
	listing, err := store.Listing()
	if err != nil {
		return report, err
	}
	head, _ := store.Head()
	byID, keptFor, kept := map[string]session.Header{}, map[string]string{}, []string{}
	for _, header := range listing.Sessions {
		byID[header.ID] = header
		if why := sessionKeeps(store, header, head.ID, lifetime, now); why != "" {
			keptFor[header.ID], kept = why, append(kept, header.ID)
		}
	}
	for ; len(kept) > 0; kept = kept[1:] {
		header := byID[kept[0]]
		links := []string{header.Parent, header.Root}
		if header.BranchedFrom != nil {
			links = append(links, header.BranchedFrom.Session)
		}
		for _, from := range links {
			if _, known := keptFor[from]; from != "" && !known {
				keptFor[from], kept = "a kept session continues it or branches from it", append(kept, from)
			}
		}
	}
	for _, header := range listing.Sessions {
		why, isKept := keptFor[header.ID]
		switch {
		case why == sessionActive:
		case isKept:
			report.Kept = append(report.Kept, sessionSkip{Session: header.ID, Reason: why})
		case dryRun:
			report.Deleted = append(report.Deleted, header.ID)
		default:
			if err := sessionRemove(store, header.ID); err != nil {
				report.Failed = append(report.Failed, sessionSkip{Session: header.ID, Reason: err.Error()})
				continue
			}
			report.Deleted = append(report.Deleted, header.ID)
		}
	}
	if dryRun {
		return report, nil
	}
	entries, err := os.ReadDir(store.Dir(""))
	for _, entry := range entries {
		if name := entry.Name(); entry.IsDir() && strings.HasPrefix(name, ".") && strings.HasSuffix(name, sessionGoneSuffix) {
			if err := os.RemoveAll(filepath.Join(store.Dir(""), name)); err != nil {
				report.Failed = append(report.Failed, sessionSkip{Session: name, Reason: err.Error()})
			}
		}
	}
	return report, err
}

func sessionKeeps(store *session.Store, header session.Header, head string, lifetime session.Lifetime, now time.Time) string {
	if !lifetime.Expired(sessionActivity(store, header, nil), now) {
		return sessionActive
	}
	events, err := store.Events(header.ID)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "its events do not read: " + err.Error()
	}
	if !lifetime.Expired(sessionActivity(store, header, events), now) {
		return sessionActive
	}
	if header.ID == head {
		return "it is the head"
	}
	var book cron.Book
	if err := book.Load(filepath.Join(store.Dir(header.ID), "cron.json")); err != nil {
		return err.Error()
	}
	if book.Live() > 0 {
		return "a cron job in it still schedules"
	}
	if err := store.Busy(header.ID); err != nil {
		return err.Error()
	}
	return ""
}

func sessionActivity(store *session.Store, header session.Header, events []session.Event) time.Time {
	entries, err := os.ReadDir(store.Dir(header.ID))
	if err != nil {
		return time.Time{}
	}
	newest := header.LastAt()
	later := func(at time.Time) {
		if at.After(newest) {
			newest = at
		}
	}
	for _, entry := range entries {
		if info, err := entry.Info(); err == nil {
			later(info.ModTime())
		}
	}
	for _, event := range events {
		later(event.At)
	}
	return newest
}

func sessionRemove(store *session.Store, id string) error {
	if err := store.Busy(id); err != nil {
		return err
	}
	gone := filepath.Join(store.Dir(""), "."+id+sessionGoneSuffix)
	if err := os.Rename(store.Dir(id), gone); err != nil {
		return err
	}
	return errors.Join(os.RemoveAll(gone), os.RemoveAll(store.AttachmentDir(id)))
}

func sessionCleanLines(page cli.Page, report sessionSweepReport) []string {
	if report.Lifetime == session.LifetimeNever {
		return page.Title("Session clean", []string{sessionStaleSetting + " is 0"}, cli.Verdict{Mark: cli.Idle, Text: "deletes nothing"})
	}
	done := "deleted"
	if report.DryRun {
		done = "would delete"
	}
	var rows []cli.Row
	for _, id := range report.Deleted {
		rows = append(rows, cli.Row{Mark: cli.Changed, Cells: []string{id, done}})
	}
	for _, kept := range report.Kept {
		rows = append(rows, cli.Row{Mark: cli.Idle, Cells: []string{kept.Session, "kept"}, Detail: kept.Reason})
	}
	for _, failed := range report.Failed {
		rows = append(rows, cli.Row{Mark: cli.Fail, Cells: []string{failed.Session, "failed"}, Detail: failed.Reason})
	}
	lines := append(page.Title("Session clean", []string{"idle past " + report.Lifetime.String(), done + " " + output.Plural(len(report.Deleted), "session")}, cli.Verdict{}), "")
	return append(lines, cli.Indent(page.Rows(rows)...)...)
}

func sessionListLines(page cli.Page, report sessionListReport, now time.Time) []string {
	if len(report.Sessions) == 0 && len(report.Skipped) == 0 {
		return page.Title("Sessions", nil, cli.Verdict{Mark: cli.Idle, Text: "none recorded"})
	}
	facts := []string{output.Plural(len(report.Sessions), "session")}
	if report.Expired > 0 {
		facts = append(facts, strconv.Itoa(report.Expired)+" idle past "+report.Lifetime.String())
	}
	var verdict cli.Verdict
	rows := make([]cli.Row, len(report.Sessions))
	for i, row := range report.Sessions {
		rows[i] = cli.Row{Mark: cli.Idle, Cells: []string{row.Handle, sessionWhen(row.At, now), output.Plural(row.Steps, "step"), row.Outcome}, Detail: output.OneLine(row.Task)}
		if row.sideOf != "" {
			rows[i].Detail = strings.Join(slices.DeleteFunc([]string{"side chat of " + row.sideOf, rows[i].Detail}, func(part string) bool { return part == "" }), " · ")
		}
		switch {
		case row.Head && report.HeadDerived:
			verdict = cli.Verdict{Mark: cli.Idle, Text: "newest " + rows[i].Cells[0]}
		case row.Head:
			rows[i].Mark, verdict = cli.Active, cli.Verdict{Mark: cli.Active, Text: "head " + rows[i].Cells[0]}
		case row.Expired:
			rows[i].Mark = cli.Warn
		}
	}
	lines := append(page.Title("Sessions", facts, verdict), "")
	lines = append(lines, cli.Indent(page.Rows(rows)...)...)
	return append(lines, skippedLines(page, report.Skipped)...)
}

func skippedLines(page cli.Page, skipped []sessionSkip) []string {
	if len(skipped) == 0 {
		return nil
	}
	rows := make([]cli.Row, len(skipped))
	for i, skip := range skipped {
		rows[i] = cli.Row{Mark: cli.Fail, Cells: []string{skip.Session}, Detail: skip.Reason}
	}
	lines := []string{"", page.Section("unreadable", cli.Verdict{Mark: cli.Fail, Text: strconv.Itoa(len(skipped))})}
	return append(lines, cli.Indent(page.Rows(rows)...)...)
}

func sessionReadsLines(page cli.Page, report sessionReadsReport) []string {
	var verdict cli.Verdict
	if report.Unrecorded > 0 {
		verdict = cli.Verdict{Mark: cli.Warn, Text: strconv.Itoa(report.Unrecorded) + " not recorded"}
	}
	lines := append(page.Title("Reads", []string{sessionHandle(report.Session, report.Name), output.Plural(len(report.Reads), "read")}, verdict), "")
	if len(report.Reads) == 0 {
		return append(lines, cli.Indent(page.Label("none recorded"))...)
	}
	rows := make([]cli.Row, len(report.Reads))
	for i, read := range report.Reads {
		rows[i] = cli.Row{Cells: []string{"step " + strconv.Itoa(read.Step), read.Tool, render.Size(read.Bytes), strings.TrimSpace(read.Source + " " + read.Span)}}
	}
	said := ""
	for i, row := range page.Rows(rows) {
		lines = append(lines, cli.Indent(row)...)
		if reasoning := report.Reads[i].Reasoning; reasoning != "" && reasoning != said {
			lines = append(lines, cli.Indent(cli.Indent(page.Label(output.OneLine(reasoning)))...)...)
		}
		said = report.Reads[i].Reasoning
	}
	return lines
}

func sessionInfoLines(page cli.Page, row sessionRow, now time.Time, store *session.Store) []string {
	verdict := cli.Verdict{Mark: cli.Idle, Text: cmp.Or(row.Outcome, "open")}
	hint := "tofu session resume " + row.Handle
	switch {
	case row.Head:
		verdict.Mark, hint = cli.Active, "tofu --continue"
	case row.Expired:
		verdict = cli.Verdict{Mark: cli.Warn, Text: verdict.Text + ", past its lifetime"}
	}
	var counts []string
	for _, count := range []struct {
		n    int
		noun string
	}{{row.Turns, "turn"}, {row.Steps, "step"}, {row.Agents, "sub-agent run"}, {row.Reads, "read"}} {
		if count.n > 0 {
			counts = append(counts, output.Plural(count.n, count.noun))
		}
	}
	ended := "open"
	if row.EndedAt != nil {
		ended = string(row.EndReason) + " · " + sessionWhen(*row.EndedAt, now)
	}
	parent := row.Parent
	if row.ForkKind != "" {
		parent += " · continues it"
	}
	facts := []cli.Fact{
		{Label: "id", Text: row.ID},
		{Label: "when", Text: sessionWhen(row.At, now)},
		{Label: "task", Text: output.OneLine(row.Task)},
		{Label: "counts", Text: strings.Join(counts, " · ")},
		{Label: "history", Text: heldText(row.Carried, row.CarriedFrom)},
		{Label: "ended", Text: ended},
		{Label: "error", Text: output.OneLine(row.Error)},
		{Label: "wire", Text: strings.TrimSpace(row.Wire + " " + row.Model)},
		{Label: "parent", Text: parent},
	}
	if row.CostUSD > 0 {
		facts = append(facts, cli.Fact{Label: "cost", Text: dollars(row.CostUSD)})
	}
	if row.Root != row.ID && row.Root != row.Parent {
		facts = append(facts, cli.Fact{Label: "root", Text: row.Root})
	}
	if row.fork != nil {
		facts = append(facts, cli.Fact{Label: "fork", Text: forkFact(store, row.fork)})
	}
	if row.AutoCompaction != "" {
		facts = append(facts, cli.Fact{Label: "context", Text: strconv.Itoa(row.ContextCeiling) + " ceiling · " +
			strconv.Itoa(row.ContextTarget) + " target · compaction " + row.AutoCompaction})
	}
	lines := append(page.Title(row.Handle, nil, verdict), "")
	lines = append(lines, cli.Indent(page.Facts(facts)...)...)
	return append(append(lines, ""), cli.Indent(page.Hint(hint))...)
}

func busyResumeLines(page cli.Page, carry sessionResume) []string {
	history, head := heldText(carry.Carried, carry.CarriedFrom), ""
	if carry.Carried > 0 {
		history += " · " + output.Plural(carry.Steps, "step")
	}
	if carry.HeadDerived {
		head = "none written, took the newest"
	}
	lines := append(page.Title("Resume", []string{carry.Handle}, cli.Verdict{Mark: cli.Warn, Text: "busy, read only"}), "")
	return append(lines, cli.Indent(page.Facts([]cli.Fact{
		{Label: "id", Text: carry.Session},
		{Label: "task", Text: output.OneLine(carry.Task)},
		{Label: "history", Text: history},
		{Label: "head", Text: head},
		{Label: "writer", Text: carry.Busy.Error()},
	})...)...)
}

func heldText(held int, from *host.CarriedFrom) string {
	origin := "fresh"
	if from != nil {
		origin = output.Plural(from.Messages, "message") + " from " + from.Handle
	}
	if held == 0 {
		return origin + ", nothing to resume"
	}
	return origin + ", holds " + output.Plural(held, "message")
}

func dollars(usd float64) string { return fmt.Sprintf("$%.6f", usd) }

func sessionShortID(id string) string { return strings.TrimPrefix(id, session.IDPrefix) }

func sessionHandle(id, name string) string { return cmp.Or(name, sessionShortID(id)) }

type (
	traceRequest       = host.TraceRequest
	traceCall          = host.TraceCall
	traceHook          = host.TraceHook
	traceMessage       = host.TraceMessage
	traceFailure       = host.TraceFailure
	sessionTraceReport = host.SessionTrace
)

type traceResult struct {
	session.ResultBody
	DurationMS int64          `json:"duration_ms"`
	Refused    bool           `json:"refused"`
	Verdict    string         `json:"gate_verdict"`
	GateError  string         `json:"gate_error"`
	Reason     *ledger.Reason `json:"gate_reason"`
	Hooks      []loop.HookRun `json:"hooks"`
}

func gateWords(result traceResult, hooks []traceHook, call string) string {
	words := result.Verdict
	if reason := result.Reason; reason != nil {
		relaxed := ""
		if reason.RelaxedBy != "" {
			relaxed = ", relaxed by " + reason.RelaxedBy
		}
		words += fmt.Sprintf(" (%s %.2f %s %.2f%s)", reason.Question, reason.Value, reason.Comparison, reason.Threshold, relaxed)
	}
	for _, ran := range hooks {
		if ran.Call == call && ran.Gate != ledger.VerdictUnset && string(ran.Gate) != result.Verdict {
			words += ", turned into " + string(ran.Gate) + " by a GateVerdict hook"
		}
	}
	if result.Reason != nil && result.Reason.AllowedBy != "" {
		words += ", allowed by " + result.Reason.AllowedBy
	}
	return strings.TrimSpace(strings.TrimSpace(words + " " + result.GateError))
}

func tracedRequests(steps []traceRequest, exchanges []session.Exchange) []traceRequest {
	if len(exchanges) == 0 {
		return steps
	}
	used := map[string]traceRequest{}
	for _, step := range steps {
		used[step.Request] = step
	}
	requests := make([]traceRequest, 0, len(exchanges))
	type sentBefore struct {
		hashes []string
		set    map[string]bool
	}
	before := map[string]sentBefore{}
	for _, exchange := range exchanges {
		request := used[exchange.Request]
		request.Request, request.Agent, request.Turn, request.Model = exchange.Request, exchange.Agent, exchange.Turn, cmp.Or(request.Model, exchange.Model)
		request.StartMS = exchange.At.UnixMilli()
		request.Why, request.Messages, request.Attempts, request.DurationMS, request.Error = strings.Trim(exchange.Why+" · "+request.Why, " ·"), len(exchange.Messages), len(exchange.Attempts), exchange.DurationMS, exchange.Error
		sent, added := before[exchange.Agent], exchange.Messages
		extends := sent.set != nil && len(sent.hashes) <= len(added) && slices.Equal(sent.hashes, added[:len(sent.hashes)])
		if extends {
			added = added[len(sent.hashes):]
		}
		for _, hash := range added {
			if !sent.set[hash] {
				request.New++
			}
		}
		if !extends {
			sent.set = make(map[string]bool, len(added))
		}
		for _, hash := range added {
			sent.set[hash] = true
		}
		sent.hashes = exchange.Messages
		before[exchange.Agent] = sent
		if last := len(exchange.Attempts) - 1; last >= 0 {
			var attempt core.Attempt
			if json.Unmarshal(exchange.Attempts[last].Detail, &attempt) == nil {
				request.Status, request.ProviderRequestID = attempt.Status, attempt.RequestID
			}
		}
		requests = append(requests, request)
	}
	return requests
}

func callReason(result session.ResultBody) string {
	if result.Error != "" || result.ExitCode == nil || *result.ExitCode == 0 {
		return result.Error
	}
	code := strconv.Itoa(*result.ExitCode)
	lines := strings.Split(strings.TrimSpace(result.Content), "\n")
	if exited := slices.IndexFunc(lines, func(line string) bool { return strings.TrimSpace(line) == "the command exited "+code }); exited >= 0 {
		lines = lines[:exited]
	}
	if len(lines) == 0 {
		return "exit " + code
	}
	failed := slices.IndexFunc(lines, failureShaped().MatchString)
	if failed < 0 {
		failed = len(lines) - 1
	}
	return strings.TrimSpace("exit " + code + ": " + strings.TrimSpace(lines[failed]))
}

func sessionTrace(store *session.Store, handle string) (sessionTraceReport, error) {
	store = store.ReadOnce()
	header, err := sessionHeader(store, handle)
	if err != nil {
		return sessionTraceReport{}, err
	}
	events, err := store.Events(header.ID)
	if err != nil {
		return sessionTraceReport{}, err
	}
	if err := loop.CheckToolOutcomes(events); err != nil {
		return sessionTraceReport{}, err
	}
	report, err := traceBody(store, header.ID, events, func(string, time.Time) bool { return true })
	if err != nil {
		return sessionTraceReport{}, err
	}
	report.Session, report.Name, report.Handle, report.Error, report.Events = header.ID, header.Named(), handleOf(store, header.ID), header.Error, len(events)
	report.Messages = slices.DeleteFunc(report.Messages, func(said traceMessage) bool { return header.CarriedFrom != nil && said.Turn == header.ID })
	report.Agents, report.Outlived = append([]session.AgentRun{}, header.Agents...), callsAfterTheLeadLeft(store, header, events)
	report, err = withAncestors(store, header, report)
	return fromSessionStart(report, header.At), err
}

func fromSessionStart(report sessionTraceReport, began time.Time) sessionTraceReport {
	for i := range report.Requests {
		report.Requests[i].StartMS -= began.UnixMilli()
	}
	for i := range report.Calls {
		report.Calls[i].StartMS -= began.UnixMilli()
	}
	for i, run := range report.Agents {
		if !run.StartedAt.IsZero() {
			report.Agents[i].StartMS = run.StartedAt.Sub(began).Milliseconds()
		}
	}
	return report
}

func startedAfter(ms int64) string { return "start_ms " + strconv.FormatInt(ms, 10) }

func traceBody(store *session.Store, id string, events []session.Event, keep func(agent string, at time.Time) bool) (sessionTraceReport, error) {
	report := sessionTraceReport{Requests: []traceRequest{}, Calls: []traceCall{}}
	events = slices.DeleteFunc(slices.Clone(events), func(event session.Event) bool { return !keep(event.Agent, event.At) })
	placed, firstCallOf := map[string]int{}, map[string]time.Time{}
	for _, event := range events {
		switch event.Kind {
		case session.EventRequest:
			var step struct {
				session.StepBody
				Phase      loop.Phase `json:"phase"`
				DurationMS int64      `json:"duration_ms"`
			}
			_ = json.Unmarshal(event.Body, &step)
			answered := cmp.Or(firstCallOf[event.ID], event.At)
			request := traceRequest{Request: event.ID, Agent: event.Agent, Turn: event.Turn, Model: step.Model, CostUSD: step.CostUSD, StartMS: answered.UnixMilli() - step.DurationMS,
				Usage: session.Usage{InputTokens: step.PromptTokens, OutputTokens: step.CompletionTokens, CacheReadTokens: step.CacheReadTokens, CacheWriteTokens: step.CacheWriteTokens}}
			if step.Phase != "" {
				request.Why = "phase " + string(step.Phase)
			}
			report.Requests = append(report.Requests, request)
		case session.EventToolCall:
			var call session.CallBody
			_ = json.Unmarshal(event.Body, &call)
			placed[event.Call] = len(report.Calls)
			if _, seen := firstCallOf[event.Request]; !seen {
				firstCallOf[event.Request] = event.At
			}
			report.Calls = append(report.Calls, traceCall{Call: event.Call, Agent: event.Agent, Turn: event.Turn, Request: event.Request, Tool: call.Tool, Args: call.Args, StartMS: event.At.UnixMilli()})
		case session.EventToolResult:
			var result traceResult
			_ = json.Unmarshal(event.Body, &result)
			if at, known := placed[event.Call]; known {
				called := &report.Calls[at]
				called.Result, called.Outcome, called.Bytes, called.Reason = event.ID, result.ToolOutcome, result.ResultBytes, callReason(result.ResultBody)
				called.DurationMS, called.Refused, called.Gate, called.Hooks = result.DurationMS, result.Refused, gateWords(result, report.Hooks, event.Call), result.Hooks
			}
		case session.EventHook:
			ran := traceHook{Agent: event.Agent, Turn: event.Turn, Call: event.Call}
			_ = json.Unmarshal(event.Body, &ran.Run)
			if at, known := placed[event.Call]; known {
				ran.Tool = report.Calls[at].Tool
			}
			report.Hooks = append(report.Hooks, ran)
		case session.EventMessage:
			var said session.MessageBody
			if event.Agent != "" || json.Unmarshal(event.Body, &said) != nil || strings.TrimSpace(said.Content) == "" || said.Role != session.RoleUser && said.Role != session.RoleAssistant {
				break
			}
			text, typed := loop.TaskIn(said.Content)
			if !typed {
				text = said.Content
			}
			report.Messages = append(report.Messages, traceMessage{ID: event.ID, Turn: event.Turn, Role: said.Role, At: event.At, Text: text})
		case session.EventTurnEnd:
			var ended struct {
				Error string `json:"error"`
			}
			if json.Unmarshal(event.Body, &ended) == nil && ended.Error != "" {
				report.Failures = append(report.Failures, traceFailure{Agent: event.Agent, Turn: event.Turn, Error: ended.Error})
			}
		}
	}
	traced, err := store.Traced(id, events)
	if err != nil {
		return sessionTraceReport{}, err
	}
	exchanges := slices.DeleteFunc(traced.Exchanges, func(exchange session.Exchange) bool {
		return !keep(exchange.Agent, exchange.At) || slices.ContainsFunc(traced.Memory, func(call session.TracedMemory) bool { return call.Request == exchange.Request })
	})
	report.Requests, report.Cache = tracedRequests(report.Requests, exchanges), cacheTrace(events, exchanges)
	if len(exchanges) > 0 {
		sizes := store.Sizes(id)
		report.Sizes = &sizes
	}
	report.Inserted, report.Changes, report.Notices, report.Memory = traced.Inserted, traced.Changes, traced.Notices, traced.Memory
	return report, nil
}

func sessionTraceLines(page cli.Page, report sessionTraceReport) []string {
	agents := make([]cli.Row, len(report.Agents))
	for i, run := range report.Agents {
		calls := 0
		for _, call := range report.Calls {
			if call.Agent == run.Agent {
				calls++
			}
		}
		agents[i] = cli.Row{Mark: cli.Idle, Cells: []string{run.Agent, cmp.Or(run.Definition, "unnamed"), cmp.Or(run.Model, "orchestrator's model"), run.Status, output.Plural(calls, "call"), dollars(run.CostUSD), startedAfter(run.StartMS)},
			Detail: "spawned by " + run.SpawnCall + " in " + run.SpawnTurn}
	}
	requests := make([]cli.Row, len(report.Requests))
	for i, request := range report.Requests {
		cells := []string{cmp.Or(request.Agent, session.AuthorOrchestrator), request.Request, request.Model, startedAfter(request.StartMS)}
		if request.Messages > 0 {
			cells = append(cells, output.Plural(request.Messages, "message")+", "+strconv.Itoa(request.New)+" new")
		}
		if words := report.Cache.Lifetimes[request.Request]; words != "" {
			cells = append(cells, words)
		}
		cells = append(cells, "in "+strconv.Itoa(request.Usage.InputTokens)+" · out "+strconv.Itoa(request.Usage.OutputTokens), dollars(request.CostUSD))
		requests[i] = cli.Row{Mark: cli.Idle, Cells: cells, Detail: request.Why}
		if request.Error != "" {
			requests[i].Mark, requests[i].Detail = cli.Fail, output.OneLine(request.Error)
		}
		requests[i].Detail = recordedIn(request.RecordedIn) + requests[i].Detail
	}
	memory := make([]cli.Row, len(report.Memory))
	for i, call := range report.Memory {
		memory[i] = cli.Row{Mark: cli.Idle, Cells: []string{cmp.Or(call.Agent, session.AuthorOrchestrator), call.Request, call.Model,
			"in " + strconv.Itoa(call.Usage.InputTokens) + " · out " + strconv.Itoa(call.Usage.OutputTokens) + " · cache read " + strconv.Itoa(call.Usage.CacheReadTokens) + " · wrote " + strconv.Itoa(call.Usage.CacheWriteTokens), dollars(call.CostUSD)}, Detail: call.Why}
	}
	calls := make([]cli.Row, len(report.Calls))
	for i, call := range report.Calls {
		calls[i] = cli.Row{Mark: cli.Done, Cells: []string{cmp.Or(call.Agent, session.AuthorOrchestrator), call.Call, call.Tool, call.Outcome, render.Size(call.Bytes), startedAfter(call.StartMS)},
			Detail: strings.TrimSpace(call.Gate + " " + output.OneLine(string(call.Args)))}
		switch {
		case call.Result == "":
			calls[i].Mark, calls[i].Cells[3] = cli.Warn, "unanswered"
		case call.Refused:
			calls[i].Mark, calls[i].Cells[3], calls[i].Detail = cli.Fail, "refused", output.OneLine(call.Reason)
		case call.Outcome == session.ToolOutcomeFailed:
			calls[i].Mark, calls[i].Detail = cli.Fail, output.OneLine(call.Reason)
		case call.Outcome == session.ToolOutcomeAborted:
			calls[i].Mark = cli.Warn
		case call.Outcome == session.ToolOutcomeSkipped:
			calls[i].Mark, calls[i].Detail = cli.Warn, output.OneLine(call.Reason)
		}
		for _, ran := range call.Hooks {
			calls[i].Detail += " · " + ran.Event + " hook ran " + strconv.Itoa(ran.Ran) + strings.TrimSuffix(" "+cmp.Or(ran.Block, ran.Ask), " ")
		}
		calls[i].Detail = recordedIn(call.RecordedIn) + calls[i].Detail
	}
	hooks := make([]cli.Row, len(report.Hooks))
	for i, ran := range report.Hooks {
		mark, detail := cli.Done, ran.Command+" · "+string(ran.Level)+" "+ran.File
		if ran.Problem != "" {
			mark, detail = cli.Warn, detail+" · "+output.OneLine(ran.Problem)
		}
		if ran.Stderr != "" {
			detail += " · stderr " + output.OneLine(ran.Stderr)
		}
		hooks[i] = cli.Row{Mark: mark, Cells: []string{cmp.Or(ran.Agent, session.AuthorOrchestrator), cmp.Or(strings.TrimSpace(ran.Call+" "+ran.Tool), "no call"), string(ran.Event),
			"exit " + strconv.Itoa(ran.Exit) + " in " + strconv.FormatInt(ran.DurationMS, 10) + " ms", cmp.Or(output.OneLine(ran.Decision), "no decision")}, Detail: detail}
	}
	inserted := make([]cli.Row, len(report.Inserted))
	for i, insert := range report.Inserted {
		when := ""
		if insert.PostedAt != nil {
			when = "posted " + insert.PostedAt.Format(time.TimeOnly) + ", "
		}
		if insert.TakenAt != nil {
			when += "taken " + insert.TakenAt.Format(time.TimeOnly)
		}
		inserted[i] = cli.Row{Mark: cli.Idle, Cells: []string{cmp.Or(insert.Agent, session.AuthorOrchestrator), insert.Origin, when, "into " + cmp.Or(insert.Request, "no request yet")},
			Detail: render.Fit(output.OneLine(insert.Text), page.Width)}
	}
	changes := make([]cli.Row, len(report.Changes))
	for i, change := range report.Changes {
		changes[i] = cli.Row{Mark: cli.Changed, Cells: []string{cmp.Or(change.Agent, session.AuthorOrchestrator), change.Kind,
			strconv.Itoa(change.Before) + " to " + output.Plural(change.After, "message")}, Detail: change.Why}
	}
	notices := make([]cli.Row, len(report.Notices))
	for i, notice := range report.Notices {
		notices[i] = cli.Row{Mark: cli.Warn, Cells: []string{cmp.Or(notice.Agent, session.AuthorOrchestrator), notice.At.Format(time.TimeOnly)}, Detail: output.OneLine(notice.Text)}
	}
	messages := make([]cli.Row, len(report.Messages))
	for i, said := range report.Messages {
		who := session.AuthorOrchestrator
		if said.Role == session.RoleUser {
			who = "to the orchestrator"
		}
		messages[i] = cli.Row{Mark: cli.Idle, Cells: []string{who, "[message" + trace.Short(said.ID) + "]", said.At.Format(time.TimeOnly)}, Detail: render.Fit(output.OneLine(said.Text), page.Width)}
	}
	failures := make([]cli.Row, len(report.Failures))
	for i, failure := range report.Failures {
		failures[i] = cli.Row{Mark: cli.Fail, Cells: []string{cmp.Or(failure.Agent, session.AuthorOrchestrator), failure.Turn}, Detail: output.OneLine(failure.Error)}
	}
	var verdict cli.Verdict
	if report.Error != "" {
		verdict = cli.Verdict{Mark: cli.Fail, Text: "ended in error"}
	}
	facts := []string{report.Handle, output.Plural(report.Events, "event")}
	if sizes := report.Sizes; sizes != nil {
		facts = append(facts, "events "+render.Size(int(sizes.Events))+", requests "+render.Size(int(sizes.Requests))+", bodies "+render.Size(int(sizes.Blobs)))
	}
	lines := page.Title("Trace", facts, verdict)
	for _, section := range []struct {
		name string
		rows []cli.Row
	}{{"sub-agents", agents}, {"sub-agents across a continue", outlivedRows(report.Outlived)}, {"requests", requests}, {"cache breaks", cacheBreakRows(report.Cache.Breaks)}, {"memory model", memory}, {"messages", messages}, {"messages tofu added", inserted}, {"calls", calls}, {"hooks", hooks}, {"list changes", changes}, {"notices", notices}, {"failures", failures}} {
		if len(section.rows) > 0 {
			lines = append(append(lines, "", page.Section(section.name, cli.Verdict{})), cli.Indent(page.Rows(section.rows)...)...)
		}
	}
	return lines
}

type requestMessage struct {
	Index    int                       `json:"index"`
	Role     string                    `json:"role"`
	Origin   string                    `json:"origin,omitempty"`
	From     *session.Origin           `json:"from,omitempty"`
	PostedAt *time.Time                `json:"posted_at,omitempty"`
	TakenAt  *time.Time                `json:"taken_at,omitempty"`
	Calls    []string                  `json:"calls,omitempty"`
	Called   []session.MessageToolCall `json:"call_details,omitempty"`
	Answers  string                    `json:"answers,omitempty"`
	Outcome  string                    `json:"tool_outcome,omitempty"`
	Text     string                    `json:"text,omitempty"`

	KeptHead   bool `json:"kept_head"`
	Breakpoint bool `json:"breakpoint"`
}

type breakpointSource string

const (
	breakpointsSent    breakpointSource = "sent"
	breakpointsEncoded breakpointSource = "encoded"
	breakpointsNone    breakpointSource = "none"
)

type requestBreakpoints struct {
	Source breakpointSource `json:"source"`
	Tools  bool             `json:"tools"`
	System []int            `json:"system,omitempty"`
	Why    string           `json:"why,omitempty"`
}

type sentBlock struct {
	Type         string          `json:"type"`
	ID           string          `json:"id"`
	ToolUseID    string          `json:"tool_use_id"`
	CacheControl json.RawMessage `json:"cache_control"`
}

type requestAttempt struct {
	core.Attempt
	Body json.RawMessage `json:"body,omitempty"`
}

type sessionRequestReport struct {
	Session     string                `json:"session"`
	Request     string                `json:"request"`
	Agent       string                `json:"agent,omitempty"`
	Turn        string                `json:"turn,omitempty"`
	At          time.Time             `json:"at"`
	Why         string                `json:"why,omitempty"`
	Wire        string                `json:"wire,omitempty"`
	Model       string                `json:"model,omitempty"`
	ToolChoice  string                `json:"tool_choice,omitempty"`
	DurationMS  int64                 `json:"duration_ms"`
	Messages    []requestMessage      `json:"messages"`
	Tools       json.RawMessage       `json:"tools,omitempty"`
	Attempts    []requestAttempt      `json:"attempts,omitempty"`
	Response    json.RawMessage       `json:"response,omitempty"`
	Error       string                `json:"error,omitempty"`
	Parts       *session.RequestParts `json:"parts,omitempty"`
	Breakpoints requestBreakpoints    `json:"breakpoints"`
}

func sessionRequest(store *session.Store, handle, request string) (sessionRequestReport, error) {
	header, err := sessionHeader(store, handle)
	if err != nil {
		return sessionRequestReport{}, err
	}
	if request == requestLast {
		rows, err := sessionRequestRows(store, header.ID)
		if err != nil {
			return sessionRequestReport{}, err
		}
		if len(rows) == 0 {
			return sessionRequestReport{}, output.ProblemError{What: "no requests recorded in this session", Hint: "tofu session trace " + handle}
		}
		request = rows[len(rows)-1].Request
	}
	view, err := store.Request(header.ID, request)
	if err != nil {
		return sessionRequestReport{}, output.ProblemError{What: err.Error(), Hint: "tofu session trace " + handle}
	}
	report := sessionRequestReport{Session: header.ID, Request: view.Request, Agent: view.Agent, Turn: view.Turn, At: view.At, Why: view.Why, Wire: view.Wire,
		Model: view.Model, ToolChoice: view.ToolChoice, DurationMS: view.DurationMS, Tools: view.Tools, Response: view.Response, Error: view.Error}
	for i, message := range view.Messages {
		shown := requestMessage{Index: i, Role: message.Role, Origin: message.Origin, From: message.From, PostedAt: message.PostedAt, TakenAt: message.TakenAt,
			Called: message.ToolCalls, Answers: message.ToolCallID, Outcome: message.ToolOutcome, Text: message.Content, KeptHead: message.KeptHead}
		for _, call := range message.ToolCalls {
			shown.Calls = append(shown.Calls, call.ID)
		}
		report.Messages = append(report.Messages, shown)
	}
	for _, attempt := range view.Attempts {
		var shown requestAttempt
		_ = json.Unmarshal(attempt.Detail, &shown.Attempt)
		shown.Body = attempt.Body
		report.Attempts = append(report.Attempts, shown)
	}
	body, source, err := breakpointBody(view, report.Attempts)
	report.Breakpoints.Source = source
	if err != nil {
		report.Breakpoints.Why = err.Error()
	}
	markBreakpoints(&report, body)
	parts, err := store.RequestParts(header.ID, view.Request)
	if err == nil {
		report.Parts = &parts
	}
	if errors.Is(err, session.ErrSentBeforeParts) {
		err = nil
	}
	return report, err
}

const requestLast = "last"

type sessionRequestRow struct {
	Request    string    `json:"request"`
	At         time.Time `json:"at"`
	Agent      string    `json:"agent,omitempty"`
	Step       int       `json:"step"`
	Model      string    `json:"model,omitempty"`
	Why        string    `json:"why,omitempty"`
	TokensIn   int       `json:"tokens_in"`
	TokensOut  int       `json:"tokens_out"`
	CacheRead  int       `json:"cache_read"`
	CacheWrite int       `json:"cache_write"`
	Error      string    `json:"error,omitempty"`
}

type sessionRequestListReport struct {
	Session  string              `json:"session"`
	Handle   string              `json:"handle"`
	Requests []sessionRequestRow `json:"requests"`
}

func sessionRequestList(store *session.Store, handle string) (sessionRequestListReport, error) {
	header, err := sessionHeader(store, handle)
	if err != nil {
		return sessionRequestListReport{}, err
	}
	rows, err := sessionRequestRows(store, header.ID)
	return sessionRequestListReport{Session: header.ID, Handle: handleOf(store, header.ID), Requests: rows}, err
}

func sessionRequestRows(store *session.Store, id string) ([]sessionRequestRow, error) {
	exchanges, err := store.Exchanges(id)
	if err != nil {
		return nil, err
	}
	events, err := store.EventsOf(id, session.EventRequest, session.EventMemoryRequest)
	if err != nil {
		return nil, err
	}
	steps := map[string]session.StepBody{}
	for _, event := range events {
		var step session.StepBody
		_ = json.Unmarshal(event.Body, &step)
		request := event.ID
		if event.Kind == session.EventMemoryRequest {
			request = cmp.Or(event.Request, event.ID)
		}
		steps[request] = step
	}
	rows := make([]sessionRequestRow, len(exchanges))
	for i, exchange := range exchanges {
		step := steps[exchange.Request]
		rows[i] = sessionRequestRow{Request: exchange.Request, At: exchange.At, Agent: exchange.Agent, Step: step.Index, Model: cmp.Or(step.Model, exchange.Model),
			Why: exchange.Why, TokensIn: step.PromptTokens, TokensOut: step.CompletionTokens, CacheRead: step.CacheReadTokens, CacheWrite: step.CacheWriteTokens, Error: exchange.Error}
	}
	slices.SortStableFunc(rows, func(a, b sessionRequestRow) int { return a.At.Compare(b.At) })
	return rows, nil
}

func sessionRequestListLines(page cli.Page, report sessionRequestListReport) []string {
	lines := append(page.Title("Requests", []string{report.Handle, output.Plural(len(report.Requests), "request")}, cli.Verdict{}), "")
	if len(report.Requests) == 0 {
		return append(lines, cli.Indent(page.Label("none recorded in this session"))...)
	}
	rows := make([]cli.Row, len(report.Requests))
	for i, request := range report.Requests {
		mark := cli.Done
		if request.Error != "" {
			mark = cli.Fail
		}
		rows[i] = cli.Row{Mark: mark, Cells: []string{loop.ShortID(request.Request), cmp.Or(request.Agent, session.AuthorOrchestrator), "step " + strconv.Itoa(request.Step),
			"in " + strconv.Itoa(request.TokensIn), "read " + strconv.Itoa(request.CacheRead), "wrote " + strconv.Itoa(request.CacheWrite)},
			Detail: traced(request.At.Format(time.DateTime), request.Model, request.Why, output.OneLine(request.Error))}
	}
	lines = append(append(lines, cli.Indent(page.Rows(rows)...)...), "")
	return append(lines, cli.Indent(page.Label("open one with tofu session request "+report.Handle+" <request id>, or last for the newest"))...)
}

const hasFlag = "--has"

func requestOrigins() []string {
	var named []string
	for _, source := range core.Source("").Enum() {
		named = append(named, string(core.Source(source).Kind())+"/"+source)
	}
	return named
}

func originName(message session.MessageBody) (string, bool) {
	origin := loop.OriginOf(message)
	return string(origin.Kind()) + "/" + string(origin.Source), message.Origin != "" || message.From != nil
}

type requestHaving struct {
	Request  string `json:"request"`
	Agent    string `json:"agent,omitempty"`
	Messages int    `json:"messages"`
	At       []int  `json:"at"`
}

type sessionRequestsHavingReport struct {
	Session  string          `json:"session"`
	Handle   string          `json:"handle"`
	Origin   string          `json:"origin"`
	Requests []requestHaving `json:"requests"`
}

func sessionRequestsHaving(store *session.Store, handle, origin string) (sessionRequestsHavingReport, error) {
	listed, err := sessionRequestList(store, handle)
	report := sessionRequestsHavingReport{Session: listed.Session, Handle: listed.Handle, Origin: origin}
	if err != nil {
		return report, err
	}
	for _, row := range listed.Requests {
		view, err := store.Request(listed.Session, row.Request)
		if err != nil {
			return report, err
		}
		having := requestHaving{Request: row.Request, Agent: row.Agent, Messages: len(view.Messages), At: []int{}}
		for i, message := range view.Messages {
			if name, marked := originName(message); marked && name == origin {
				having.At = append(having.At, i)
			}
		}
		report.Requests = append(report.Requests, having)
	}
	return report, nil
}

func sessionRequestsHavingLines(page cli.Page, report sessionRequestsHavingReport) []string {
	carrying := 0
	rows := make([]cli.Row, len(report.Requests))
	for i, request := range report.Requests {
		positions := make([]string, len(request.At))
		for j, at := range request.At {
			positions[j] = strconv.Itoa(at)
		}
		mark, where := cli.Idle, "none"
		if len(positions) > 0 {
			carrying++
			mark, where = cli.Done, "at "+strings.Join(positions, ", ")
		}
		rows[i] = cli.Row{Mark: mark, Cells: []string{loop.ShortID(request.Request), cmp.Or(request.Agent, session.AuthorOrchestrator), output.Plural(request.Messages, "message"), where}}
	}
	lines := append(page.Title("Requests", []string{report.Handle, output.Plural(len(report.Requests), "request"), strconv.Itoa(carrying) + " from " + report.Origin}, cli.Verdict{}), "")
	if len(rows) == 0 {
		return append(lines, cli.Indent(page.Label("none recorded in this session"))...)
	}
	return append(lines, cli.Indent(page.Rows(rows)...)...)
}

func breakpointBody(view session.RequestView, attempts []requestAttempt) (json.RawMessage, breakpointSource, error) {
	if len(attempts) > 0 && len(attempts[len(attempts)-1].Body) > 0 {
		return attempts[len(attempts)-1].Body, breakpointsSent, nil
	}
	if view.Wire != wiring.WireSubscription {
		return nil, breakpointsNone, nil
	}
	reading := session.Reading{Messages: make([]session.Message, len(view.Messages))}
	for i, message := range view.Messages {
		reading.Messages[i].MessageBody = message
	}
	conversation, err := loop.ConversationOf(reading)
	if err != nil {
		return nil, breakpointsNone, err
	}
	var tools []core.Tool
	if len(view.Tools) > 0 {
		if err := json.Unmarshal(view.Tools, &tools); err != nil {
			return nil, breakpointsNone, err
		}
	}
	var system []string
	var messages []core.Message
	for _, message := range conversation {
		if message.Role == core.RoleSystem {
			system = append(system, message.Content)
			continue
		}
		messages = append(messages, message)
	}
	body, err := anthropic.Request{Model: view.Model, System: system, Messages: messages, Tools: tools}.Encode(true)
	return body, breakpointsEncoded, err
}

func markBreakpoints(report *sessionRequestReport, body json.RawMessage) {
	var sent struct {
		System   []sentBlock `json:"system"`
		Tools    []sentBlock `json:"tools"`
		Messages []struct {
			Role       string          `json:"role"`
			ToolCallID string          `json:"tool_call_id"`
			Content    json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if json.Unmarshal(body, &sent) != nil {
		return
	}
	system, answered, called, spoken := -1, map[string]int{}, map[string]int{}, []int{}
	for i, message := range report.Messages {
		switch {
		case message.Role == session.RoleSystem:
			system = i
		case message.Answers != "":
			answered[message.Answers] = i
		case message.Role != session.RoleAssistant || len(message.Calls) > 0 || !core.BlankText(message.Text):
			spoken = append(spoken, i)
		}
		for _, id := range message.Calls {
			called[id] = i
		}
	}
	lookup := func(index map[string]int, id string) int {
		if at, ok := index[id]; ok {
			return at
		}
		return -1
	}
	mark := func(at int) {
		if at >= 0 {
			report.Messages[at].Breakpoint = true
		}
	}
	for i, block := range sent.System {
		if block.CacheControl != nil {
			report.Breakpoints.System = append(report.Breakpoints.System, i)
			mark(system)
		}
	}
	report.Breakpoints.Tools = slices.ContainsFunc(sent.Tools, func(tool sentBlock) bool { return tool.CacheControl != nil })
	plain := 0
	for _, message := range sent.Messages {
		var blocks []sentBlock
		_ = json.Unmarshal(message.Content, &blocks)
		owner := -1
		switch {
		case message.Role == session.RoleSystem:
			owner = system
		case message.ToolCallID != "":
			owner = lookup(answered, message.ToolCallID)
		case len(blocks) > 0 && blocks[0].ToolUseID != "":
		case plain < len(spoken):
			owner, plain = spoken[plain], plain+1
		}
		for _, block := range blocks {
			switch {
			case block.CacheControl == nil:
			case block.ToolUseID != "":
				mark(lookup(answered, block.ToolUseID))
			case block.Type == "tool_use":
				mark(lookup(called, block.ID))
			default:
				mark(owner)
			}
		}
	}
}

func breakpointsText(report sessionRequestReport) string {
	var at []string
	if report.Breakpoints.Tools {
		at = append(at, "the last tool")
	}
	for _, block := range report.Breakpoints.System {
		at = append(at, "system block "+strconv.Itoa(block))
	}
	for _, message := range report.Messages {
		if message.Breakpoint && message.Role != session.RoleSystem {
			at = append(at, "message "+strconv.Itoa(message.Index))
		}
	}
	switch {
	case report.Breakpoints.Why != "":
		return "not worked out: " + report.Breakpoints.Why
	case len(at) == 0:
		return "none: the " + cmp.Or(report.Wire, "unnamed") + " wire marked no cache breakpoint on this request"
	}
	from := ", read off the body sent"
	if report.Breakpoints.Source == breakpointsEncoded {
		from = ", where the anthropic wire places them, encoded again because no body was kept"
	}
	return output.Plural(len(at), "breakpoint") + ": " + strings.Join(at, ", ") + from
}

func requestPartsLines(page cli.Page, parts *session.RequestParts) []string {
	lines := []string{"", page.Section("what went into it", cli.Verdict{})}
	if parts == nil {
		return append(lines, cli.Indent("not recorded: this request was sent before tofu kept what goes into each request")...)
	}
	rows := make([]cli.Row, len(parts.Prompt))
	for i, part := range parts.Prompt {
		why, appended := strings.CutSuffix(part.Why, loop.PartAppendedWhy)
		placed := "system prompt"
		switch {
		case appended:
			placed, why = "in a message after the cached system prompt, changed mid-session", strings.TrimSuffix(why, "; ")
		case part.PerTurn:
			placed = "with the task"
		}
		cells := []string{part.Kind, cmp.Or(part.Rule, part.Agent, "tofu"), placed, strconv.Itoa(part.Tokens) + " tokens"}
		if part.Off {
			cells = append(cells, "switched off")
		}
		rows[i] = cli.Row{Mark: cli.Idle, Cells: cells, Detail: strings.Join(slices.DeleteFunc([]string{why, part.File}, func(said string) bool { return said == "" }), " · ")}
	}
	var skills, memories []string
	for _, item := range parts.Carried {
		if item.From != nil && item.From.Kind == string(core.OriginSkill) && !slices.Contains(skills, item.From.Name) {
			skills = append(skills, item.From.Name)
		}
		for _, id := range item.Memory {
			if !slices.Contains(memories, id) {
				memories = append(memories, id)
			}
		}
	}
	var tokens []string
	for _, kind := range slices.Sorted(maps.Keys(parts.Sent.Tokens)) {
		tokens = append(tokens, kind+" "+strconv.Itoa(parts.Sent.Tokens[kind]))
	}
	lines = append(lines, cli.Indent(page.Rows(rows)...)...)
	return append(lines, cli.Indent(page.Facts([]cli.Fact{
		{Label: "skills loaded", Text: cmp.Or(strings.Join(skills, ", "), "none")},
		{Label: "memory", Text: cmp.Or(strings.Join(memories, ", "), "none")},
		{Label: "tokens", Text: strings.Join(tokens, " · ")},
		{Label: "images", Text: strconv.Itoa(parts.Sent.Images)},
		{Label: "since the last", Text: output.Plural(len(parts.Sent.Added), "message") + " new, " + strconv.Itoa(len(parts.Sent.Dropped)) + " dropped"},
	})...)...)
}

func wireMessageLines(body json.RawMessage, width int) []string {
	var sent struct {
		Messages []json.RawMessage `json:"messages"`
		Input    []json.RawMessage `json:"input"`
	}
	if json.Unmarshal(body, &sent) != nil {
		return nil
	}
	var lines []string
	for i, raw := range append(sent.Messages, sent.Input...) {
		var item struct {
			Role    string          `json:"role"`
			Type    string          `json:"type"`
			CallID  string          `json:"call_id"`
			Content json.RawMessage `json:"content"`
		}
		_ = json.Unmarshal(raw, &item)
		var blocks []struct {
			Type      string `json:"type"`
			ID        string `json:"id"`
			ToolUseID string `json:"tool_use_id"`
			Text      string `json:"text"`
		}
		_ = json.Unmarshal(item.Content, &blocks)
		parts, said := []string{strings.TrimSpace(item.Role + " " + item.Type + " " + item.CallID)}, ""
		for _, block := range blocks {
			if block.Type == "text" {
				said += " " + output.OneLine(block.Text)
				continue
			}
			parts = append(parts, strings.TrimSpace(block.Type+" "+block.ID+block.ToolUseID))
		}
		line := "messages." + strconv.Itoa(i) + " " + strings.Join(parts, " | ")
		if said != "" {
			line += " | text" + render.Fit(said, max(width-render.Cells(line)-len(" | text"), 0))
		}
		lines = append(lines, line)
	}
	return lines
}

func sessionRequestLines(page cli.Page, report sessionRequestReport) []string {
	verdict := cli.Verdict{Mark: cli.Done, Text: "answered"}
	if report.Error != "" {
		verdict = cli.Verdict{Mark: cli.Fail, Text: "failed"}
	}
	lines := append(page.Title("Request", []string{report.Request, output.Plural(len(report.Messages), "message")}, verdict), "")
	lines = append(lines, cli.Indent(page.Facts([]cli.Fact{
		{Label: "agent", Text: cmp.Or(report.Agent, session.AuthorOrchestrator)},
		{Label: "turn", Text: report.Turn},
		{Label: "why", Text: report.Why},
		{Label: "at", Text: report.At.Format(time.DateTime) + " · " + strconv.FormatInt(report.DurationMS, 10) + " ms"},
		{Label: "wire", Text: strings.TrimSpace(report.Wire + " " + report.Model + " " + report.ToolChoice)},
		{Label: "cache", Text: breakpointsText(report)},
	})...)...)
	rows := make([]cli.Row, len(report.Messages))
	for i, message := range report.Messages {
		cells := []string{strconv.Itoa(message.Index), message.Role}
		if len(message.Calls) > 0 {
			cells = append(cells, "calls "+strings.Join(message.Calls, ", "))
		}
		if message.Answers != "" {
			cells = append(cells, "answers "+message.Answers+" "+message.Outcome)
		}
		if origin, marked := originName(session.MessageBody{Role: message.Role, Origin: message.Origin, From: message.From}); marked {
			cells = append(cells, "from "+origin)
		}
		if message.KeptHead {
			cells = append(cells, "kept head")
		}
		if message.Breakpoint {
			cells = append(cells, "breakpoint")
		}
		rows[i] = cli.Row{Mark: cli.Idle, Cells: cells, Detail: render.Fit(output.OneLine(message.Text), page.Width)}
		if message.Role == session.RoleSystem {
			rows[i].Detail = render.Size(len(message.Text)) + " of system prompt, whole in --json"
		}
	}
	lines = append(append(lines, requestPartsLines(page, report.Parts)...), "", page.Section("messages tofu built", cli.Verdict{}))
	lines = append(lines, cli.Indent(page.Rows(rows)...)...)
	for i, attempt := range report.Attempts {
		facts := []cli.Fact{
			{Label: "status", Text: strconv.Itoa(attempt.Status) + " · " + attempt.RequestID},
			{Label: "timing", Text: strconv.FormatInt(attempt.HeadersMS, 10) + " ms to headers, " + strconv.FormatInt(attempt.StreamMS, 10) + " ms streaming, " + render.Size(int(attempt.Received))},
			{Label: "retry after", Text: attempt.RetryAfter},
			{Label: "error", Text: attempt.Error},
			{Label: "error body", Text: attempt.ErrorBody},
		}
		lines = append(append(lines, "", page.Section("attempt "+strconv.Itoa(i+1)+" "+attempt.URL, cli.Verdict{})), cli.Indent(page.Facts(facts)...)...)
		lines = append(lines, cli.Indent(wireMessageLines(attempt.Body, page.Width)...)...)
	}
	if report.Error != "" {
		return append(append(lines, "", page.Section("error", cli.Verdict{Mark: cli.Fail})), cli.Indent(report.Error)...)
	}
	var answer core.Decision
	_ = json.Unmarshal(report.Response, &answer)
	var calls []string
	for _, call := range answer.ToolCalls {
		calls = append(calls, call.ID+" "+call.Name)
	}
	return append(append(lines, "", page.Section("response", cli.Verdict{})), cli.Indent(page.Facts([]cli.Fact{
		{Label: "stop", Text: answer.Stop + " · " + answer.RequestID},
		{Label: "usage", Text: fmt.Sprintf("in %d · out %d · cache read %d · cache write %d · first token %d ms",
			answer.Usage.InputTokens, answer.Usage.OutputTokens, answer.CacheReadTokens, answer.CacheWriteTokens, answer.FirstTokenMS)},
		{Label: "calls", Text: strings.Join(calls, ", ")},
		{Label: "text", Text: output.OneLine(answer.Content)},
	})...)...)
}

func sessionWhen(at, now time.Time) string {
	since := now.Sub(at)
	switch {
	case since < 0, since >= sessionWeek:
		return at.Format(sessionDate)
	case since < sessionDay:
		return render.Until(since) + " ago"
	}
	return at.Format(sessionClock)
}
