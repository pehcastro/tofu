package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strconv"
	"strings"
	"time"

	"tofu/interface/cli"
	"tofu/internal/host"
	"tofu/internal/llm"
	"tofu/internal/session"
	"tofu/internal/turn"
	"tofu/internal/widget"
)

const (
	sessionSubcommands = "tofu session list|info|trace|reads|resume|rename <name|id> [--json]"
	sessionFresh       = "no session recorded here"
	sessionDay         = 24 * time.Hour
	sessionWeek        = 7 * sessionDay
	sessionClock       = "Mon 15:04"
	sessionDate        = "2 Jan 15:04"
)

type sessionSkip struct {
	Session string `json:"session"`
	Reason  string `json:"reason"`
}

type sessionRow struct {
	ID               string    `json:"id"`
	Name             string    `json:"name,omitempty"`
	At               time.Time `json:"at"`
	Task             string    `json:"task,omitempty"`
	Turns            int       `json:"turns"`
	Agents           int       `json:"sub_agents"`
	Steps            int       `json:"steps"`
	Carried          int       `json:"carried_messages"`
	Outcome          string    `json:"outcome,omitempty"`
	Wire             string    `json:"wire,omitempty"`
	Model            string    `json:"model,omitempty"`
	Parent           string    `json:"parent,omitempty"`
	Root             string    `json:"root,omitempty"`
	ForkedInto       string    `json:"forked_into,omitempty"`
	ForkIntoKind     string    `json:"fork_into_kind,omitempty"`
	ForkKind         string    `json:"fork_kind,omitempty"`
	ContextCeiling   int       `json:"context_ceiling,omitempty"`
	ContextTarget    int       `json:"context_target,omitempty"`
	AutoCompaction   string    `json:"auto_compaction,omitempty"`
	ForkTokensBefore int       `json:"fork_tokens_before,omitempty"`
	ForkTokensAfter  int       `json:"fork_tokens_after,omitempty"`
	CostUSD          float64   `json:"cost_usd,omitempty"`
	Head             bool      `json:"head,omitempty"`

	Reads      int               `json:"reads"`
	Unrecorded int               `json:"unrecorded_reads,omitempty"`
	EndedAt    *time.Time        `json:"ended_at,omitempty"`
	EndReason  session.EndReason `json:"end_reason,omitempty"`
	Expired    bool              `json:"expired,omitempty"`

	lastAt time.Time
	tasks  []string
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
	Task        string             `json:"task,omitempty"`
	Outcome     string             `json:"outcome,omitempty"`
	Steps       int                `json:"steps"`
	Carried     int                `json:"carried_messages"`
	HeadDerived bool               `json:"head_derived,omitempty"`
	Fresh       string             `json:"fresh,omitempty"`
	Busy        *session.BusyError `json:"busy,omitempty"`

	messages []llm.Message
	tasks    []string
}

func sessionOperands(subcommand string) (string, int, bool) {
	switch subcommand {
	case "list":
		return "", 0, true
	case "info", "trace", "reads", "resume":
		return "<name|id>", 1, true
	case "rename":
		return "<name|id> <new name>", 2, true
	}
	return "", 0, false
}

func sessionVerb(args []string, in io.Reader, out, errOut io.Writer) int {
	handles, _, err := verbArgs(args[min(1, len(args)):])
	o := verbOutput{verb: "session", usageLine: sessionSubcommands, asJSON: jsonAsked(args), out: out, errOut: errOut}
	if len(withoutJSON(args)) == 0 {
		return o.usage(errors.New("no subcommand"))
	}
	operands, wanted, known := sessionOperands(args[0])
	if !known {
		return o.usage(fmt.Errorf("there is no subcommand %q", args[0]))
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
	now := time.Now()
	lifetime := session.DefaultSettings().Lifetime
	switch args[0] {
	case "list":
		report, err := sessionListing(store, lifetime, now)
		if err != nil {
			return o.fail(err)
		}
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
	if code := o.done(carry.Busy == nil, carry, func(page cli.Page) []string { return resumeLines(page, carry) }); code != exitOK || o.asJSON {
		return code
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
	row, messages, err := sessionDetail(store, id)
	if err != nil {
		return sessionResume{}, err
	}
	carry := sessionResume{
		Session:  row.ID,
		Name:     row.Name,
		Task:     row.Task,
		Outcome:  row.Outcome,
		Steps:    row.Steps,
		Carried:  row.Carried,
		messages: messages,
		tasks:    row.tasks,
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
	return host.Carry{Session: carry.Session, Name: carry.Name, Messages: carry.messages, Tasks: carry.tasks}
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
	row.Name = *header.Name
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
	row := sessionRow{
		ID:               header.ID,
		At:               header.At,
		Task:             header.Task,
		Turns:            max(header.Turns, len(tasks)),
		Agents:           len(header.Agents),
		Steps:            len(reading.Steps),
		Reads:            len(reading.Reads),
		Carried:          len(messages),
		Outcome:          header.Outcome,
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
		lastAt:           header.LastAt(),
		tasks:            tasks,
	}
	if header.Name != nil {
		row.Name = *header.Name
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
	for _, header := range listing.Sessions {
		row, _, err := sessionDetail(store, header.ID)
		if err != nil {
			report.Skipped = append(report.Skipped, sessionSkip{Session: header.ID, Reason: err.Error()})
			continue
		}
		row.Head = row.ID == report.Head
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
		rows[i] = cli.Row{Mark: cli.Idle, Cells: []string{sessionHandle(row.ID, row.Name), sessionWhen(row.At, now), sessionSteps(row.Steps), row.Outcome}, Detail: oneLine(row.Task)}
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
	hint := "tofu session resume " + sessionHandle(row.ID, row.Name)
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
	lines := append(page.Title(sessionHandle(row.ID, row.Name), nil, verdict), "")
	lines = append(lines, cli.Indent(page.Facts(facts)...)...)
	return append(append(lines, ""), cli.Indent(page.Hint(hint))...)
}

func resumeLines(page cli.Page, carry sessionResume) []string {
	if carry.Fresh != "" {
		lines := append(page.Title("Resume", nil, cli.Verdict{Mark: cli.Idle, Text: "starts fresh"}), "")
		return append(lines, cli.Indent(page.Facts([]cli.Fact{{Label: "reason", Text: carry.Fresh}})...)...)
	}
	carried, head, writer := "none, starts over", "", ""
	verdict := cli.Verdict{Mark: cli.Active, Text: cmp.Or(carry.Outcome, "open")}
	if carry.Carried > 0 {
		carried = countOf(carry.Carried, "message") + " · " + sessionSteps(carry.Steps)
	}
	if carry.HeadDerived {
		head = "none written, took the newest"
	}
	if carry.Busy != nil {
		verdict, writer = cli.Verdict{Mark: cli.Warn, Text: "busy, read only"}, carry.Busy.Error()
	}
	lines := append(page.Title("Resume", []string{sessionHandle(carry.Session, carry.Name)}, verdict), "")
	return append(lines, cli.Indent(page.Facts([]cli.Fact{
		{Label: "id", Text: carry.Session},
		{Label: "task", Text: oneLine(carry.Task)},
		{Label: "carried", Text: carried},
		{Label: "head", Text: head},
		{Label: "writer", Text: writer},
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

type traceRequest struct {
	Request string        `json:"request"`
	Agent   string        `json:"agent,omitempty"`
	Turn    string        `json:"turn"`
	Model   string        `json:"model,omitempty"`
	Usage   session.Usage `json:"usage"`
	CostUSD float64       `json:"cost_usd"`
}

type traceCall struct {
	Call    string `json:"call"`
	Agent   string `json:"agent,omitempty"`
	Turn    string `json:"turn"`
	Request string `json:"request,omitempty"`
	Tool    string `json:"tool"`
	Result  string `json:"result,omitempty"`
	Outcome string `json:"outcome,omitempty"`
	Bytes   int    `json:"result_bytes"`
}

type sessionTraceReport struct {
	Session  string             `json:"session"`
	Name     string             `json:"name,omitempty"`
	Events   int                `json:"events"`
	Agents   []session.AgentRun `json:"agents"`
	Requests []traceRequest     `json:"requests"`
	Calls    []traceCall        `json:"calls"`
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
	report := sessionTraceReport{Session: header.ID, Events: len(events), Agents: append([]session.AgentRun{}, header.Agents...), Requests: []traceRequest{}, Calls: []traceCall{}}
	if header.Name != nil {
		report.Name = *header.Name
	}
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
			report.Calls = append(report.Calls, traceCall{Call: event.Call, Agent: event.Agent, Turn: event.Turn, Request: event.Request, Tool: call.Tool})
		case session.EventToolResult:
			var result session.ResultBody
			_ = json.Unmarshal(event.Body, &result)
			if at, known := placed[event.Call]; known {
				report.Calls[at].Result, report.Calls[at].Outcome, report.Calls[at].Bytes = event.ID, result.ToolOutcome, result.ResultBytes
			}
		}
	}
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
		requests[i] = cli.Row{Mark: cli.Idle, Cells: []string{cmp.Or(request.Agent, session.AuthorOrchestrator), request.Request, request.Model,
			"in " + strconv.Itoa(request.Usage.InputTokens) + " · out " + strconv.Itoa(request.Usage.OutputTokens), dollars(request.CostUSD)}}
	}
	calls := make([]cli.Row, len(report.Calls))
	for i, call := range report.Calls {
		calls[i] = cli.Row{Mark: cli.Done, Cells: []string{cmp.Or(call.Agent, session.AuthorOrchestrator), call.Call, call.Tool, call.Outcome, widget.Size(call.Bytes)}, Detail: call.Result}
		if call.Result == "" {
			calls[i].Mark, calls[i].Cells[3] = cli.Warn, "unanswered"
		}
	}
	lines := page.Title("Trace", []string{sessionHandle(report.Session, report.Name), countOf(report.Events, "event")}, cli.Verdict{})
	for _, section := range []struct {
		name string
		rows []cli.Row
	}{{"sub-agents", agents}, {"requests", requests}, {"calls", calls}} {
		if len(section.rows) > 0 {
			lines = append(append(lines, "", page.Section(section.name, cli.Verdict{})), cli.Indent(page.Rows(section.rows)...)...)
		}
	}
	return lines
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
