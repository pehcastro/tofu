package main

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strconv"
	"strings"
	"time"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/session"
	"tofu/internal/turn"
	"tofu/internal/widget"
)

const (
	sessionUsage = "usage: tofu session list | tofu session info <name|id> | tofu session reads <name|id> | tofu session resume <name|id> | tofu session rename <name|id> <new name>, each with --json"

	sessionNone     = "no session has been recorded in this directory yet"
	sessionFresh    = "no session is recorded here, so this starts fresh"
	sessionDerived  = "no head is written, so this is the newest session nothing continues"
	sessionEmpty    = "the record holds no message, so this starts the work again rather than continuing it"
	sessionNoReads  = "this session read nothing, or was recorded before reads were kept"
	sessionKeptWhen = "an expired session is listed and never deleted: delete one yourself when you want it gone"
)

const (
	sessionHandleColumn   = 19
	sessionIDColumn       = 22
	sessionWhenColumn     = 9
	sessionStepsColumn    = 8
	sessionOutcomeColumn  = 14
	sessionReadToolColumn = 15
	sessionDay            = 24 * time.Hour
	sessionWeek           = 7 * sessionDay
	sessionClock          = "Mon 15:04"
	sessionDate           = "2 Jan 15:04"
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
	Steps            int       `json:"steps"`
	Carried          int       `json:"carried_messages"`
	Outcome          string    `json:"outcome,omitempty"`
	Wire             string    `json:"wire,omitempty"`
	Model            string    `json:"model,omitempty"`
	Parent           string    `json:"parent,omitempty"`
	Root             string    `json:"root,omitempty"`
	ForkedInto       string    `json:"forked_into,omitempty"`
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
}

type sessionListReport struct {
	Head        string           `json:"head,omitempty"`
	HeadDerived bool             `json:"head_derived,omitempty"`
	Lifetime    session.Lifetime `json:"lifetime"`
	Expired     int              `json:"expired"`
	Sessions    []sessionRow     `json:"sessions"`
	Skipped     []sessionSkip    `json:"skipped,omitempty"`
	ReportedAt  time.Time        `json:"reported_at"`
}

type sessionReadsReport struct {
	Session    string         `json:"session"`
	Name       string         `json:"name,omitempty"`
	Reads      []session.Read `json:"reads"`
	Unrecorded int            `json:"unrecorded"`
	ReportedAt time.Time      `json:"reported_at"`
}

type sessionResume struct {
	Session     string `json:"session,omitempty"`
	Name        string `json:"name,omitempty"`
	Task        string `json:"task,omitempty"`
	Outcome     string `json:"outcome,omitempty"`
	Steps       int    `json:"steps"`
	Carried     int    `json:"carried_messages"`
	HeadDerived bool   `json:"head_derived,omitempty"`
	Fresh       string `json:"fresh,omitempty"`

	messages []llm.Message
}

func sessionVerb(args []string, in io.Reader, out, errOut io.Writer, shade palette) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(errOut, sessionUsage)
		return exitUsage
	}
	handles, asJSON, err := sessionArgs(args[1:])
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "tofu session: %v\n%s\n", err, sessionUsage)
		return exitUsage
	}
	store, err := session.Open()
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "tofu session: %v\n", err)
		return exitVerdict
	}
	now := time.Now()
	settings := session.DefaultSettings()
	switch args[0] {
	case "list":
		if len(handles) != 0 {
			_, _ = fmt.Fprintf(errOut, "tofu session list: %q, and the list takes no session: tofu session info %s\n", handles[0], handles[0])
			return exitUsage
		}
		report, err := sessionListing(store, settings.Lifetime, now)
		if err != nil {
			_, _ = fmt.Fprintf(errOut, "tofu session list: %v\n", err)
			return exitVerdict
		}
		return sessionPrint(out, errOut, asJSON, report, sessionListText(report, shade, now))
	case "info":
		if len(handles) != 1 {
			_, _ = fmt.Fprintln(errOut, "tofu session info: which session? a name or an id, and tofu session list names them")
			return exitUsage
		}
		row, _, err := sessionDetail(store, handles[0])
		if err != nil {
			_, _ = fmt.Fprintf(errOut, "tofu session info: %v\n", err)
			return exitVerdict
		}
		if head, headErr := store.Head(); headErr == nil {
			row.Head = head.ID == row.ID
		}
		row.Expired = settings.Lifetime.Expired(row.lastAt, now)
		return sessionPrint(out, errOut, asJSON, row, sessionInfoText(row, shade, now))
	case "reads":
		if len(handles) != 1 {
			_, _ = fmt.Fprintln(errOut, "tofu session reads: which session? a name or an id, and tofu session list names them")
			return exitUsage
		}
		report, err := sessionReads(store, handles[0], now)
		if err != nil {
			_, _ = fmt.Fprintf(errOut, "tofu session reads: %v\n", err)
			return exitVerdict
		}
		return sessionPrint(out, errOut, asJSON, report, sessionReadsText(report, shade))
	case "rename":
		if len(handles) != 2 {
			_, _ = fmt.Fprintln(errOut, "tofu session rename: which session, and what to call it? tofu session rename <name|id> <new name>")
			return exitUsage
		}
		row, err := sessionRenamed(store, handles[0], handles[1])
		if err != nil {
			_, _ = fmt.Fprintf(errOut, "tofu session rename: %v\n", err)
			return exitVerdict
		}
		return sessionPrint(out, errOut, asJSON, row, sessionInfoText(row, shade, now))
	case "resume":
		if len(handles) != 1 {
			_, _ = fmt.Fprintln(errOut, "tofu session resume: which session? a name or an id, and tofu --continue takes the head")
			return exitUsage
		}
		carry, err := resumeOf(store, handles[0])
		if err != nil {
			_, _ = fmt.Fprintf(errOut, "tofu session resume: %v\n", err)
			return exitVerdict
		}
		return startResumed(carry, asJSON, in, out, errOut)
	}
	_, _ = fmt.Fprintf(errOut, "tofu session: there is no subcommand %q\n%s\n", args[0], sessionUsage)
	return exitUsage
}

func continueVerb(args []string, in io.Reader, out, errOut io.Writer) int {
	handles, asJSON, err := sessionArgs(args)
	if err != nil || len(handles) != 0 {
		_, _ = fmt.Fprintln(errOut, "usage: tofu --continue takes the head, and tofu session resume <name|id> takes any other")
		return exitUsage
	}
	store, err := session.Open()
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "tofu --continue: %v\n", err)
		return exitVerdict
	}
	return startResumed(continueCarry(store), asJSON, in, out, errOut)
}

func sessionArgs(args []string) ([]string, bool, error) {
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

func sessionPrint(out, errOut io.Writer, asJSON bool, report any, text string) int {
	if !asJSON {
		_, _ = fmt.Fprint(out, text)
		return exitOK
	}
	if err := writeJSON(out, report); err != nil {
		_, _ = fmt.Fprintf(errOut, "tofu session: %v\n", err)
		return exitVerdict
	}
	return exitOK
}

func startResumed(carry sessionResume, asJSON bool, in io.Reader, out, errOut io.Writer) int {
	if code := sessionPrint(out, errOut, asJSON, carry, resumeText(carry)); code != exitOK {
		return code
	}
	return appVerb(in, out, errOut, carry)
}

func continueCarry(store *session.Store) sessionResume {
	head, err := store.Head()
	if err != nil {
		return sessionResume{Fresh: sessionFresh}
	}
	carry, err := resumeOf(store, head.ID)
	if err != nil {
		return sessionResume{Fresh: "the head " + head.ID + " does not read, so this starts fresh: " + err.Error()}
	}
	carry.HeadDerived = head.Derived
	return carry
}

func resumeOf(store *session.Store, id string) (sessionResume, error) {
	row, messages, err := sessionDetail(store, id)
	if err != nil {
		return sessionResume{}, err
	}
	return sessionResume{
		Session:  row.ID,
		Name:     row.Name,
		Task:     row.Task,
		Outcome:  row.Outcome,
		Steps:    row.Steps,
		Carried:  row.Carried,
		messages: messages,
	}, nil
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
		return session.Header{}, fmt.Errorf("there is no session %s here, and tofu session list names the ones there are", handle)
	}
	if err != nil {
		return session.Header{}, err
	}
	if len(matched) > 1 {
		return session.Header{}, errors.New(sessionAmbiguous(handle, matched))
	}
	return matched[0], nil
}

func sessionReads(store *session.Store, handle string, now time.Time) (sessionReadsReport, error) {
	header, err := sessionHeader(store, handle)
	if err != nil {
		return sessionReadsReport{}, err
	}
	reads, err := store.ReadsOf(header.ID)
	if err != nil {
		return sessionReadsReport{}, err
	}
	report := sessionReadsReport{
		Session:    header.ID,
		Reads:      reads.Reads,
		Unrecorded: reads.Unrecorded,
		ReportedAt: now,
	}
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
	row := sessionRow{
		ID:               header.ID,
		At:               header.At,
		Task:             header.Task,
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
	}
	if header.Name != nil {
		row.Name = *header.Name
	}
	for _, event := range events {
		switch event.Kind {
		case session.EventStep:
			row.Steps++
		case session.EventRead:
			row.Reads++
		}
	}
	return row, messages, nil
}

func sessionListing(store *session.Store, lifetime session.Lifetime, now time.Time) (sessionListReport, error) {
	listing, err := store.Listing()
	if err != nil {
		return sessionListReport{}, err
	}
	report := sessionListReport{Lifetime: lifetime, ReportedAt: now}
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

func sessionListText(report sessionListReport, shade palette, now time.Time) string {
	if len(report.Sessions) == 0 && len(report.Skipped) == 0 {
		return sessionNone + "\n"
	}
	headID := sessionShortID(report.Head)
	counted := strconv.Itoa(len(report.Sessions)) + " sessions, newest first"
	if len(report.Sessions) == 1 {
		counted = "1 session"
	}
	var body strings.Builder
	body.WriteString(headline(counted, shade.settled(headID), len(headID)) + "\n\n")
	for _, row := range report.Sessions {
		body.WriteString(sessionLine(row, shade, now) + "\n")
	}
	if report.HeadDerived {
		body.WriteString("\n" + reportIndent + sessionDerived + "\n")
	}
	if report.Expired > 0 {
		body.WriteString("\n" + reportIndent + strconv.Itoa(report.Expired) + " past the " + report.Lifetime.String() + " lifetime. " + sessionKeptWhen + "\n")
	}
	return body.String() + sessionSkipText(report.Skipped)
}

func sessionReadsText(report sessionReadsReport, shade palette) string {
	counted := strconv.Itoa(len(report.Reads)) + " reads"
	if len(report.Reads) == 1 {
		counted = "1 read"
	}
	var body strings.Builder
	handle := cmp.Or(report.Name, sessionShortID(report.Session))
	body.WriteString(headline(counted, shade.settled(handle), len(handle)) + "\n\n")
	said := ""
	for _, read := range report.Reads {
		columns := column("step "+strconv.Itoa(read.Step), sessionStepsColumn) +
			column(read.Tool, sessionReadToolColumn) +
			column(widget.Size(read.Bytes), sessionWhenColumn)
		source := strings.TrimRight(read.Source+" "+read.Span, " ")
		body.WriteString(reportIndent + columns + widget.Fit(source, konst.ProseWidthChars-len(reportIndent)-widget.Cells(columns)) + "\n")
		if read.Reasoning != "" && read.Reasoning != said {
			body.WriteString(paragraph("said", read.Reasoning))
		}
		said = read.Reasoning
	}
	if len(report.Reads) == 0 {
		body.WriteString(reportIndent + sessionNoReads + "\n")
	}
	if report.Unrecorded > 0 {
		body.WriteString("\n" + reportIndent + strconv.Itoa(report.Unrecorded) + " reads happened in this session and were not recorded as reads\n")
	}
	return body.String()
}

func sessionAmbiguous(handle string, matched []session.Header) string {
	body := strconv.Itoa(len(matched)) + " sessions are called " + handle + ", so say which by id:"
	for _, header := range matched {
		line := "\n" + reportIndent + column(header.ID, sessionIDColumn) + column(header.At.Format(sessionDate), sessionWhenColumn)
		body += line + widget.Fit(oneLine(header.Task), konst.ProseWidthChars-widget.Cells(line))
	}
	return body
}

func sessionLine(row sessionRow, shade palette, now time.Time) string {
	handle := sessionShortID(row.ID)
	if row.Name != "" {
		handle = widget.Fit(row.Name, sessionHandleColumn)
	}
	if row.Head {
		handle = shade.settled(handle)
	}
	columns := column(handle, sessionHandleColumn) +
		column(sessionWhen(row.At, now), sessionWhenColumn) +
		column(sessionSteps(row.Steps), sessionStepsColumn) +
		column(row.Outcome, sessionOutcomeColumn)
	task := widget.Fit(oneLine(row.Task), konst.ProseWidthChars-len(reportIndent)-widget.Cells(columns))
	return reportIndent + strings.TrimRight(columns+task, " ")
}

func column(text string, width int) string {
	return widget.Pad(text, width) + "  "
}

func paragraph(label, text string) string {
	return strings.Join(wrapped(label, text), "\n") + "\n"
}

func sessionSteps(steps int) string {
	if steps == 1 {
		return "1 step"
	}
	return strconv.Itoa(steps) + " steps"
}

func sessionShortID(id string) string { return strings.TrimPrefix(id, session.IDPrefix) }

func sessionSkipText(skipped []sessionSkip) string {
	if len(skipped) == 0 {
		return ""
	}
	noun := " sessions the store could not read:\n"
	if len(skipped) == 1 {
		noun = " session the store could not read:\n"
	}
	body := "\n" + strconv.Itoa(len(skipped)) + noun
	for _, skip := range skipped {
		body += reportIndent + skip.Session + "  " + skip.Reason + "\n"
	}
	return body
}

func sessionInfoText(row sessionRow, shade palette, now time.Time) string {
	var body strings.Builder
	body.WriteString(headline(cmp.Or(row.Name, row.ID), shade.settled(row.Outcome), len(row.Outcome)) + "\n\n")
	body.WriteString(labelled("id", row.ID) + "\n")
	body.WriteString(labelled("when", row.At.Format(sessionDate)+", "+sessionWhen(row.At, now)) + "\n")
	if row.Task != "" {
		body.WriteString(paragraph("task", row.Task))
	}
	body.WriteString(labelled("counts", sessionSteps(row.Steps)+", "+strconv.Itoa(row.Carried)+" messages a resume would send, "+strconv.Itoa(row.Reads)+" reads recorded") + "\n")
	if row.EndedAt != nil {
		body.WriteString(labelled("ended", string(row.EndReason)+", "+row.EndedAt.Format(sessionDate)) + "\n")
	} else {
		body.WriteString(labelled("ended", "no, this session is open and a resume continues it") + "\n")
	}
	if row.Expired {
		body.WriteString(paragraph("expired", "past its lifetime. "+sessionKeptWhen))
	}
	if row.Wire != "" {
		body.WriteString(labelled("wire", strings.TrimSuffix(row.Wire+" "+row.Model, " ")) + "\n")
	}
	if row.CostUSD > 0 {
		body.WriteString(labelled("cost", fmt.Sprintf("$%.6f", row.CostUSD)) + "\n")
	}
	if lineage := sessionLineage(row); lineage != "" {
		body.WriteString(paragraph("lineage", lineage))
	}
	if row.AutoCompaction != "" {
		body.WriteString(paragraph("context", strconv.Itoa(row.ContextCeiling)+" token ceiling, "+
			strconv.Itoa(row.ContextTarget)+" token target, automatic compaction "+row.AutoCompaction))
	}
	if row.Head {
		body.WriteString(labelled("head", "tofu --continue resumes this one") + "\n")
	}
	if row.Carried == 0 {
		body.WriteString(paragraph("resume", sessionEmpty))
	}
	return body.String()
}

func sessionLineage(row sessionRow) string {
	var parts []string
	if row.Parent != "" {
		began := "spawned by "
		if row.ForkKind != "" {
			began = "continues "
		}
		parts = append(parts, began+row.Parent)
	}
	if row.Root != "" && row.Root != row.ID && row.Root != row.Parent {
		parts = append(parts, "rooted at "+row.Root)
	}
	if row.ForkedInto != "" {
		into := "forked into " + row.ForkedInto
		if row.ForkKind != "" {
			into += " as a " + row.ForkKind
		}
		parts = append(parts, into)
	}
	if row.ForkTokensBefore > 0 {
		parts = append(parts, strconv.Itoa(row.ForkTokensBefore)+" tokens at the fork, "+strconv.Itoa(row.ForkTokensAfter)+" after it")
	}
	return strings.Join(parts, ", ")
}

func resumeText(carry sessionResume) string {
	if carry.Fresh != "" {
		return carry.Fresh + "\n"
	}
	state := cmp.Or(carry.Outcome, "resumable")
	var body strings.Builder
	body.WriteString(headline("continuing "+cmp.Or(carry.Name, carry.Session), state, len(state)) + "\n\n")
	if carry.Name != "" {
		body.WriteString(labelled("id", carry.Session) + "\n")
	}
	if carry.Task != "" {
		body.WriteString(paragraph("task", carry.Task))
	}
	body.WriteString(labelled("carried", strconv.Itoa(carry.Carried)+" messages from "+sessionSteps(carry.Steps)) + "\n")
	if carry.HeadDerived {
		body.WriteString(labelled("head", sessionDerived) + "\n")
	}
	if carry.Carried == 0 {
		body.WriteString(paragraph("resume", sessionEmpty))
	}
	return body.String()
}

func sessionWhen(at, now time.Time) string {
	since := now.Sub(at)
	switch {
	case since < 0, since >= sessionWeek:
		return at.Format(sessionDate)
	case since < time.Minute:
		return "just now"
	case since < time.Hour:
		return strconv.Itoa(int(since.Minutes())) + "m ago"
	case since < sessionDay:
		return strconv.Itoa(int(since.Hours())) + "h ago"
	}
	return at.Format(sessionClock)
}
