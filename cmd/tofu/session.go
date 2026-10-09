package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/interface/cli"
	"tofu/interface/tui/trace"
	"tofu/internal/host"
	"tofu/internal/judge/ledger"
	"tofu/internal/llm"
	"tofu/internal/session"
	"tofu/internal/turn"
	"tofu/internal/widget"
)

const (
	sessionSubcommands = "tofu session <name|id>, or tofu session list [--all], or tofu session info|trace|reads|resume|rename|request|find|branch <name|id> [--json]"
	sessionFresh       = "no session recorded here"
	sessionDay         = 24 * time.Hour
	sessionWeek        = 7 * sessionDay
	sessionClock       = "Mon 15:04"
	sessionDate        = "2 Jan 15:04"
)

type sessionSkip = host.SessionSkip

type sessionRow struct {
	host.SessionInfo
	lastAt time.Time
	tasks  []string
	sideOf string
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
	HeadDerived bool               `json:"head_derived,omitempty"`
	Fresh       string             `json:"fresh,omitempty"`
	Busy        *session.BusyError `json:"busy,omitempty"`

	messages []llm.Message
	tasks    []string
	read     *session.Store
}

func sessionOperands(subcommand string) (string, int, bool) {
	switch subcommand {
	case "list":
		return "", 0, true
	case "info", "trace", "reads", "resume":
		return "<name|id>", 1, true
	case "rename":
		return "<name|id> <new name>", 2, true
	case "request":
		return "<name|id> <request id>", 2, true
	}
	return "", 0, false
}

func sessionVerb(args []string, in io.Reader, out, errOut io.Writer) int {
	all := len(args) > 0 && args[0] == "list" && slices.Contains(args, "--all")
	args = slices.DeleteFunc(slices.Clone(args), func(arg string) bool { return all && arg == "--all" })
	handles, _, err := verbArgs(args[min(1, len(args)):])
	o := verbOutput{verb: "session", usageLine: sessionSubcommands, asJSON: jsonAsked(args), out: out, errOut: errOut}
	if len(withoutJSON(args)) == 0 {
		return o.usage(errors.New("no subcommand"))
	}
	now := time.Now()
	if args[0] == "branch" {
		o.verb, o.usageLine = "session branch", sessionBranchUsage
		ask, err := sessionBranchArgs(args[1:])
		if err != nil {
			return o.usage(err)
		}
		return sessionReport(o, func(store *session.Store) (sessionBranchReport, error) { return sessionBranch(store, ask) }, sessionBranchLines)
	}
	if args[0] == "find" {
		o.verb, o.usageLine = "session find", sessionFindUsage
		handle, query, err := sessionFindArgs(args[1:], now)
		if err != nil {
			return o.usage(err)
		}
		return sessionReport(o, func(store *session.Store) (sessionFindReport, error) { return sessionFind(store, handle, query) },
			func(page cli.Page, report sessionFindReport) []string { return sessionFindLines(page, report, now) })
	}
	operands, wanted, known := sessionOperands(args[0])
	if !known {
		o.usageLine = "tofu session <name|id> [--json]"
		if handles, _, err = verbArgs(args); err == nil && len(handles) != 1 {
			err = fmt.Errorf("%d operands, want <name|id>", len(handles))
		}
		if err != nil {
			return o.usage(err)
		}
		return sessionReport(o, func(store *session.Store) (sessionFamilyReport, error) { return sessionFamilyOf(store, handles[0]) },
			func(page cli.Page, report sessionFamilyReport) []string { return sessionFamilyLines(page, report, now) })
	}
	o.verb, o.usageLine = "session "+args[0], strings.TrimSpace("tofu session "+args[0]+" "+operands)+" [--json]"
	if err == nil && len(handles) != wanted {
		err = fmt.Errorf("%d operands, want %s", len(handles), cmp.Or(operands, "none"))
	}
	if err != nil {
		return o.usage(err)
	}
	store, err := session.Open()
	if err != nil {
		return o.fail(err)
	}
	lifetime := session.DefaultSettings().Lifetime
	switch args[0] {
	case "list":
		report, err := sessionListing(store, lifetime, now)
		if err != nil {
			return o.fail(err)
		}
		report.Sessions = slices.DeleteFunc(report.Sessions, func(row sessionRow) bool { return !all && row.sideOf != "" })
		return o.done(true, report, func(page cli.Page) []string { return sessionListLines(page, report, now) })
	case "info":
		row, _, err := sessionDetail(store, handles[0])
		if err != nil {
			return o.fail(err)
		}
		if head, headErr := store.Head(); headErr == nil {
			row.Head = head.ID == row.ID
		}
		row.Expired = lifetime.Expired(row.lastAt, now)
		return o.done(true, row, func(page cli.Page) []string { return sessionInfoLines(page, row, now) })
	case "trace":
		report, err := sessionTrace(store, handles[0])
		if err != nil {
			return o.fail(err)
		}
		return o.done(true, report, func(page cli.Page) []string { return sessionTraceLines(page, report) })
	case "reads":
		report, err := sessionReads(store, handles[0])
		if err != nil {
			return o.fail(err)
		}
		return o.done(true, report, func(page cli.Page) []string { return sessionReadsLines(page, report) })
	case "request":
		report, err := sessionRequest(store, handles[0], handles[1])
		if err != nil {
			return o.fail(err)
		}
		return o.done(true, report, func(page cli.Page) []string { return sessionRequestLines(page, report) })
	case "rename":
		row, err := sessionRenamed(store, handles[0], handles[1])
		if err != nil {
			return o.fail(err)
		}
		return o.done(true, row, func(page cli.Page) []string {
			return append([]string{page.Glyph(cli.Changed) + " renamed " + row.ID + " to " + row.Name},
				cli.Indent(page.Hint("tofu session info "+row.Name))...)
		})
	}
	carry, err := resumeOf(store, handles[0])
	if err != nil {
		return o.fail(err)
	}
	return startResumed(o, carry, in)
}

func continueVerb(args []string, in io.Reader, out, errOut io.Writer) int {
	handles, asJSON, err := verbArgs(args)
	o := verbOutput{verb: "--continue", usageLine: "tofu --continue [--json], or tofu session resume <name|id>", asJSON: asJSON, out: out, errOut: errOut}
	if err == nil && len(handles) != 0 {
		err = fmt.Errorf("takes the head, not %q", handles[0])
	}
	if err != nil {
		return o.usage(err)
	}
	store, err := session.Open()
	if err != nil {
		return o.fail(err)
	}
	return startResumed(o, continueCarry(store), in)
}

func verbArgs(args []string) ([]string, bool, error) {
	var handles []string
	asJSON := false
	for _, arg := range args {
		switch {
		case arg == jsonFlag:
			asJSON = true
		case strings.HasPrefix(arg, "-"):
			return nil, false, fmt.Errorf("unknown argument %q", arg)
		default:
			handles = append(handles, arg)
		}
	}
	return handles, asJSON, nil
}

func startResumed(o verbOutput, carry sessionResume, in io.Reader) int {
	if o.asJSON || carry.Busy != nil {
		return o.done(carry.Busy == nil, carry, func(page cli.Page) []string { return busyResumeLines(page, carry) })
	}
	return appVerb(in, o.out, o.errOut, carry)
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
	store = store.ReadOnce()
	row, messages, err := sessionDetail(store, id)
	if err != nil {
		return sessionResume{}, err
	}
	carry := sessionResume{
		Session:  row.ID,
		Name:     row.Name,
		Handle:   row.Handle,
		Task:     row.Task,
		Outcome:  row.Outcome,
		Steps:    row.Steps,
		Carried:  row.Carried,
		messages: messages,
		tasks:    row.tasks,
		read:     store,
	}
	var busy session.BusyError
	switch err := store.Busy(row.ID); {
	case errors.As(err, &busy):
		carry.Busy = &busy
	case err != nil:
		return sessionResume{}, err
	}
	return carry, nil
}

func (carry sessionResume) hosted() host.Carry {
	return host.Carry{Session: carry.Session, Name: carry.Name, Messages: carry.messages, Tasks: carry.tasks, Store: carry.read}
}

func sessionRenamed(store *session.Store, handle, to string) (sessionRow, error) {
	row, _, err := sessionDetail(store, handle)
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

func sessionHeader(store *session.Store, handle string) (session.Header, error) {
	matched, err := store.Resolve(handle)
	if errors.Is(err, fs.ErrNotExist) {
		return session.Header{}, problemError{What: "no session " + handle + " here", Hint: "tofu session list"}
	}
	if err != nil {
		return session.Header{}, err
	}
	if len(matched) > 1 {
		ids := make([]string, len(matched))
		for i, header := range matched {
			ids[i] = header.ID + " (" + sessionWhen(header.At, time.Now()) + ")"
		}
		return session.Header{}, problemError{What: strconv.Itoa(len(matched)) + " sessions are called " + handle + ": " + strings.Join(ids, ", "), Hint: "name one by id"}
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

func sessionDetail(store *session.Store, handle string) (sessionRow, []llm.Message, error) {
	header, err := sessionHeader(store, handle)
	if err != nil {
		return sessionRow{}, nil, err
	}
	events, err := store.Body(header.ID)
	if err != nil {
		return sessionRow{}, nil, err
	}
	messages, err := turn.ConversationFrom(events)
	if err != nil {
		return sessionRow{}, nil, err
	}
	reading, err := session.ReadEvents(events)
	if err != nil {
		return sessionRow{}, nil, err
	}
	var tasks []string
	for _, event := range events {
		var outcome struct {
			Task string `json:"task"`
		}
		if event.Kind == session.EventOutcome && json.Unmarshal(event.Body, &outcome) == nil {
			tasks = append(tasks, outcome.Task)
		}
	}
	row := sessionRow{SessionInfo: host.SessionInfo{
		ID:               header.ID,
		At:               header.At,
		LastAt:           host.LastAt(store, header.ID),
		Task:             header.Task,
		Turns:            max(header.Turns, len(tasks)),
		Agents:           len(header.Agents),
		Steps:            len(reading.Steps),
		Reads:            len(reading.Reads),
		Carried:          len(messages),
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
	}, lastAt: header.LastAt(), tasks: tasks}
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
	if header.ForkedInto != "" {
		if into, err := store.Header(header.ForkedInto); err == nil {
			row.ForkIntoKind = into.ForkKind
		}
		steps, err := contextSteps(events)
		if err != nil {
			return sessionRow{}, nil, err
		}
		for _, step := range steps {
			if step.Fork != nil {
				row.ForkIntoKind = string(step.Fork.Kind)
			}
		}
	}
	return row, messages, nil
}

func sessionListing(store *session.Store, lifetime session.Lifetime, now time.Time) (sessionListReport, error) {
	listing, err := store.Listing()
	if err != nil {
		return sessionListReport{}, err
	}
	report := sessionListReport{Lifetime: lifetime, Sessions: []sessionRow{}}
	if head, err := store.Head(); err == nil {
		report.Head, report.HeadDerived = head.ID, head.Derived
	}
	for _, skip := range listing.Skipped {
		report.Skipped = append(report.Skipped, sessionSkip{Session: skip.ID, Reason: skip.Reason.Error()})
	}
	for _, family := range listing.Families() {
		row, _, err := sessionDetail(store, family.Session)
		if err != nil {
			report.Skipped = append(report.Skipped, sessionSkip{Session: family.Session, Reason: err.Error()})
			continue
		}
		row.Head, row.Generations = slices.ContainsFunc(family.Generations, func(header session.Header) bool { return header.ID == report.Head }), len(family.Generations)
		row.Expired = lifetime.Expired(row.lastAt, now)
		if row.Expired {
			report.Expired++
		}
		report.Sessions = append(report.Sessions, row)
	}
	return report, nil
}

func sessionListLines(page cli.Page, report sessionListReport, now time.Time) []string {
	if len(report.Sessions) == 0 && len(report.Skipped) == 0 {
		return page.Title("Sessions", nil, cli.Verdict{Mark: cli.Idle, Text: "none recorded"})
	}
	facts := []string{countOf(len(report.Sessions), "session")}
	if report.Expired > 0 {
		facts = append(facts, strconv.Itoa(report.Expired)+" past "+report.Lifetime.String()+", kept")
	}
	var verdict cli.Verdict
	rows := make([]cli.Row, len(report.Sessions))
	for i, row := range report.Sessions {
		rows[i] = cli.Row{Mark: cli.Idle, Cells: []string{row.Handle, sessionWhen(row.At, now), sessionSteps(row.Steps), row.Outcome}, Detail: oneLine(row.Task)}
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
	lines := append(page.Title("Reads", []string{sessionHandle(report.Session, report.Name), countOf(len(report.Reads), "read")}, verdict), "")
	if len(report.Reads) == 0 {
		return append(lines, cli.Indent(page.Label("none recorded"))...)
	}
	rows := make([]cli.Row, len(report.Reads))
	for i, read := range report.Reads {
		rows[i] = cli.Row{Cells: []string{"step " + strconv.Itoa(read.Step), read.Tool, widget.Size(read.Bytes), strings.TrimSpace(read.Source + " " + read.Span)}}
	}
	said := ""
	for i, row := range page.Rows(rows) {
		lines = append(lines, cli.Indent(row)...)
		if reasoning := report.Reads[i].Reasoning; reasoning != "" && reasoning != said {
			lines = append(lines, cli.Indent(cli.Indent(page.Label(oneLine(reasoning)))...)...)
		}
		said = report.Reads[i].Reasoning
	}
	return lines
}

func sessionInfoLines(page cli.Page, row sessionRow, now time.Time) []string {
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
			counts = append(counts, countOf(count.n, count.noun))
		}
	}
	carried := "none, a resume starts over"
	if row.Carried > 0 {
		carried = countOf(row.Carried, "message")
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
		{Label: "task", Text: oneLine(row.Task)},
		{Label: "counts", Text: strings.Join(counts, " · ")},
		{Label: "carried", Text: carried},
		{Label: "ended", Text: ended},
		{Label: "error", Text: oneLine(row.Error)},
		{Label: "wire", Text: strings.TrimSpace(row.Wire + " " + row.Model)},
		{Label: "parent", Text: parent},
	}
	if row.CostUSD > 0 {
		facts = append(facts, cli.Fact{Label: "cost", Text: dollars(row.CostUSD)})
	}
	if row.Root != row.ID && row.Root != row.Parent {
		facts = append(facts, cli.Fact{Label: "root", Text: row.Root})
	}
	if row.ForkedInto != "" {
		fork := &contextForkReport{Into: row.ForkedInto, Kind: row.ForkIntoKind}
		if row.ForkTokensBefore > 0 {
			fork.Counts = &contextForkCounts{TokensBefore: row.ForkTokensBefore, TokensAfter: row.ForkTokensAfter}
		}
		facts = append(facts, cli.Fact{Label: "fork", Text: forkFact(fork)})
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
	carried, head := "none, starts over", ""
	if carry.Carried > 0 {
		carried = countOf(carry.Carried, "message") + " · " + sessionSteps(carry.Steps)
	}
	if carry.HeadDerived {
		head = "none written, took the newest"
	}
	lines := append(page.Title("Resume", []string{carry.Handle}, cli.Verdict{Mark: cli.Warn, Text: "busy, read only"}), "")
	return append(lines, cli.Indent(page.Facts([]cli.Fact{
		{Label: "id", Text: carry.Session},
		{Label: "task", Text: oneLine(carry.Task)},
		{Label: "carried", Text: carried},
		{Label: "head", Text: head},
		{Label: "writer", Text: carry.Busy.Error()},
	})...)...)
}

func countOf(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}

func sessionSteps(steps int) string { return countOf(steps, "step") }

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
	Hooks      []turn.HookRun `json:"hooks"`
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
	before := map[string][]string{}
	for _, exchange := range exchanges {
		request := used[exchange.Request]
		request.Request, request.Agent, request.Turn, request.Model = exchange.Request, exchange.Agent, exchange.Turn, cmp.Or(request.Model, exchange.Model)
		request.Why, request.Messages, request.Attempts, request.DurationMS, request.Error = exchange.Why, len(exchange.Messages), len(exchange.Attempts), exchange.DurationMS, exchange.Error
		for _, hash := range exchange.Messages {
			if !slices.Contains(before[exchange.Agent], hash) {
				request.New++
			}
		}
		before[exchange.Agent] = exchange.Messages
		if last := len(exchange.Attempts) - 1; last >= 0 {
			var attempt llm.Attempt
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
	printed := strings.TrimSuffix(strings.TrimSpace(result.Content), "the command exited "+code)
	lines := strings.Split(strings.TrimSpace(printed), "\n")
	return strings.TrimSpace("exit " + code + ": " + lines[len(lines)-1])
}

func sessionTrace(store *session.Store, handle string) (sessionTraceReport, error) {
	header, err := sessionHeader(store, handle)
	if err != nil {
		return sessionTraceReport{}, err
	}
	events, err := store.Events(header.ID)
	if err != nil {
		return sessionTraceReport{}, err
	}
	report, err := traceBody(store, header.ID, events, func(string, time.Time) bool { return true })
	if err != nil {
		return sessionTraceReport{}, err
	}
	report.Session, report.Name, report.Handle, report.Error, report.Events = header.ID, header.Named(), handleOf(store, header.ID), header.Error, len(events)
	report.Messages = slices.DeleteFunc(report.Messages, func(said traceMessage) bool { return header.CarriedFrom != nil && said.Turn == header.ID })
	report.Agents, report.Outlived = append([]session.AgentRun{}, header.Agents...), callsAfterTheLeadLeft(store, header, events)
	return withAncestors(store, header, report)
}

func traceBody(store *session.Store, id string, events []session.Event, keep func(agent string, at time.Time) bool) (sessionTraceReport, error) {
	report := sessionTraceReport{Requests: []traceRequest{}, Calls: []traceCall{}}
	events = slices.DeleteFunc(slices.Clone(events), func(event session.Event) bool { return !keep(event.Agent, event.At) })
	placed := map[string]int{}
	for _, event := range events {
		switch event.Kind {
		case session.EventRequest:
			var step session.StepBody
			_ = json.Unmarshal(event.Body, &step)
			report.Requests = append(report.Requests, traceRequest{Request: event.ID, Agent: event.Agent, Turn: event.Turn, Model: step.Model, CostUSD: step.CostUSD,
				Usage: session.Usage{InputTokens: step.PromptTokens, OutputTokens: step.CompletionTokens, CacheReadTokens: step.CacheReadTokens, CacheWriteTokens: step.CacheWriteTokens}})
		case session.EventToolCall:
			var call session.CallBody
			_ = json.Unmarshal(event.Body, &call)
			placed[event.Call] = len(report.Calls)
			report.Calls = append(report.Calls, traceCall{Call: event.Call, Agent: event.Agent, Turn: event.Turn, Request: event.Request, Tool: call.Tool, Args: call.Args})
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
			text, typed := turn.TaskIn(said.Content)
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
		agents[i] = cli.Row{Mark: cli.Idle, Cells: []string{run.Agent, cmp.Or(run.Definition, "unnamed"), cmp.Or(run.Model, "orchestrator's model"), run.Status, countOf(calls, "call"), dollars(run.CostUSD)},
			Detail: "spawned by " + run.SpawnCall + " in " + run.SpawnTurn}
	}
	requests := make([]cli.Row, len(report.Requests))
	for i, request := range report.Requests {
		cells := []string{cmp.Or(request.Agent, session.AuthorOrchestrator), request.Request, request.Model}
		if request.Messages > 0 {
			cells = append(cells, countOf(request.Messages, "message")+", "+strconv.Itoa(request.New)+" new")
		}
		if words := report.Cache.Lifetimes[request.Request]; words != "" {
			cells = append(cells, words)
		}
		cells = append(cells, "in "+strconv.Itoa(request.Usage.InputTokens)+" · out "+strconv.Itoa(request.Usage.OutputTokens), dollars(request.CostUSD))
		requests[i] = cli.Row{Mark: cli.Idle, Cells: cells, Detail: request.Why}
		if request.Error != "" {
			requests[i].Mark, requests[i].Detail = cli.Fail, oneLine(request.Error)
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
		calls[i] = cli.Row{Mark: cli.Done, Cells: []string{cmp.Or(call.Agent, session.AuthorOrchestrator), call.Call, call.Tool, call.Outcome, widget.Size(call.Bytes)},
			Detail: strings.TrimSpace(call.Gate + " " + oneLine(string(call.Args)))}
		switch {
		case call.Result == "":
			calls[i].Mark, calls[i].Cells[3] = cli.Warn, "unanswered"
		case call.Refused:
			calls[i].Mark, calls[i].Cells[3], calls[i].Detail = cli.Fail, "refused", oneLine(call.Reason)
		case call.Outcome == session.ToolOutcomeFailed:
			calls[i].Mark, calls[i].Detail = cli.Fail, oneLine(call.Reason)
		case call.Outcome == session.ToolOutcomeAborted:
			calls[i].Mark = cli.Warn
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
			mark, detail = cli.Warn, detail+" · "+oneLine(ran.Problem)
		}
		if ran.Stderr != "" {
			detail += " · stderr " + oneLine(ran.Stderr)
		}
		hooks[i] = cli.Row{Mark: mark, Cells: []string{cmp.Or(ran.Agent, session.AuthorOrchestrator), cmp.Or(strings.TrimSpace(ran.Call+" "+ran.Tool), "no call"), string(ran.Event),
			"exit " + strconv.Itoa(ran.Exit) + " in " + strconv.FormatInt(ran.DurationMS, 10) + " ms", cmp.Or(oneLine(ran.Decision), "no decision")}, Detail: detail}
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
			Detail: widget.Fit(oneLine(insert.Text), page.Width)}
	}
	changes := make([]cli.Row, len(report.Changes))
	for i, change := range report.Changes {
		changes[i] = cli.Row{Mark: cli.Changed, Cells: []string{cmp.Or(change.Agent, session.AuthorOrchestrator), change.Kind,
			strconv.Itoa(change.Before) + " to " + countOf(change.After, "message")}, Detail: change.Why}
	}
	notices := make([]cli.Row, len(report.Notices))
	for i, notice := range report.Notices {
		notices[i] = cli.Row{Mark: cli.Warn, Cells: []string{cmp.Or(notice.Agent, session.AuthorOrchestrator), notice.At.Format(time.TimeOnly)}, Detail: oneLine(notice.Text)}
	}
	messages := make([]cli.Row, len(report.Messages))
	for i, said := range report.Messages {
		who := session.AuthorOrchestrator
		if said.Role == session.RoleUser {
			who = "to the orchestrator"
		}
		messages[i] = cli.Row{Mark: cli.Idle, Cells: []string{who, "[message" + trace.Short(said.ID) + "]", said.At.Format(time.TimeOnly)}, Detail: widget.Fit(oneLine(said.Text), page.Width)}
	}
	failures := make([]cli.Row, len(report.Failures))
	for i, failure := range report.Failures {
		failures[i] = cli.Row{Mark: cli.Fail, Cells: []string{cmp.Or(failure.Agent, session.AuthorOrchestrator), failure.Turn}, Detail: oneLine(failure.Error)}
	}
	var verdict cli.Verdict
	if report.Error != "" {
		verdict = cli.Verdict{Mark: cli.Fail, Text: "ended in error"}
	}
	facts := []string{report.Handle, countOf(report.Events, "event")}
	if sizes := report.Sizes; sizes != nil {
		facts = append(facts, "events "+widget.Size(int(sizes.Events))+", requests "+widget.Size(int(sizes.Requests))+", bodies "+widget.Size(int(sizes.Blobs)))
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
	PostedAt *time.Time                `json:"posted_at,omitempty"`
	TakenAt  *time.Time                `json:"taken_at,omitempty"`
	Calls    []string                  `json:"calls,omitempty"`
	Called   []session.MessageToolCall `json:"call_details,omitempty"`
	Answers  string                    `json:"answers,omitempty"`
	Outcome  string                    `json:"tool_outcome,omitempty"`
	Text     string                    `json:"text,omitempty"`
}

type requestAttempt struct {
	llm.Attempt
	Body json.RawMessage `json:"body,omitempty"`
}

type sessionRequestReport struct {
	Session    string           `json:"session"`
	Request    string           `json:"request"`
	Agent      string           `json:"agent,omitempty"`
	Turn       string           `json:"turn,omitempty"`
	At         time.Time        `json:"at"`
	Why        string           `json:"why,omitempty"`
	Wire       string           `json:"wire,omitempty"`
	Model      string           `json:"model,omitempty"`
	ToolChoice string           `json:"tool_choice,omitempty"`
	DurationMS int64            `json:"duration_ms"`
	Messages   []requestMessage `json:"messages"`
	Tools      json.RawMessage  `json:"tools,omitempty"`
	Attempts   []requestAttempt `json:"attempts,omitempty"`
	Response   json.RawMessage  `json:"response,omitempty"`
	Error      string           `json:"error,omitempty"`
}

func sessionRequest(store *session.Store, handle, request string) (sessionRequestReport, error) {
	header, err := sessionHeader(store, handle)
	if err != nil {
		return sessionRequestReport{}, err
	}
	view, err := store.Request(header.ID, request)
	if err != nil {
		return sessionRequestReport{}, problemError{What: err.Error(), Hint: "tofu session trace " + handle}
	}
	report := sessionRequestReport{Session: header.ID, Request: view.Request, Agent: view.Agent, Turn: view.Turn, At: view.At, Why: view.Why, Wire: view.Wire,
		Model: view.Model, ToolChoice: view.ToolChoice, DurationMS: view.DurationMS, Tools: view.Tools, Response: view.Response, Error: view.Error}
	for i, message := range view.Messages {
		shown := requestMessage{Index: i, Role: message.Role, Origin: message.Origin, PostedAt: message.PostedAt, TakenAt: message.TakenAt,
			Called: message.ToolCalls, Answers: message.ToolCallID, Outcome: message.ToolOutcome, Text: message.Content}
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
	return report, nil
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
				said += " " + oneLine(block.Text)
				continue
			}
			parts = append(parts, strings.TrimSpace(block.Type+" "+block.ID+block.ToolUseID))
		}
		line := "messages." + strconv.Itoa(i) + " " + strings.Join(parts, " | ")
		if said != "" {
			line += " | text" + widget.Fit(said, max(width-widget.Cells(line)-len(" | text"), 0))
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
	lines := append(page.Title("Request", []string{report.Request, countOf(len(report.Messages), "message")}, verdict), "")
	lines = append(lines, cli.Indent(page.Facts([]cli.Fact{
		{Label: "agent", Text: cmp.Or(report.Agent, session.AuthorOrchestrator)},
		{Label: "turn", Text: report.Turn},
		{Label: "why", Text: report.Why},
		{Label: "at", Text: report.At.Format(time.DateTime) + " · " + strconv.FormatInt(report.DurationMS, 10) + " ms"},
		{Label: "wire", Text: strings.TrimSpace(report.Wire + " " + report.Model + " " + report.ToolChoice)},
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
		if message.Origin != "" {
			cells = append(cells, "added by tofu: "+message.Origin)
		}
		rows[i] = cli.Row{Mark: cli.Idle, Cells: cells, Detail: widget.Fit(oneLine(message.Text), page.Width)}
		if message.Role == session.RoleSystem {
			rows[i].Detail = widget.Size(len(message.Text)) + " of system prompt, whole in --json"
		}
	}
	lines = append(append(lines, "", page.Section("messages tofu built", cli.Verdict{})), cli.Indent(page.Rows(rows)...)...)
	for i, attempt := range report.Attempts {
		facts := []cli.Fact{
			{Label: "status", Text: strconv.Itoa(attempt.Status) + " · " + attempt.RequestID},
			{Label: "timing", Text: strconv.FormatInt(attempt.HeadersMS, 10) + " ms to headers, " + strconv.FormatInt(attempt.StreamMS, 10) + " ms streaming, " + widget.Size(int(attempt.Received))},
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
	var answer llm.Decision
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
		{Label: "text", Text: oneLine(answer.Content)},
	})...)...)
}

func sessionWhen(at, now time.Time) string {
	since := now.Sub(at)
	switch {
	case since < 0, since >= sessionWeek:
		return at.Format(sessionDate)
	case since < sessionDay:
		return widget.Until(since) + " ago"
	}
	return at.Format(sessionClock)
}
