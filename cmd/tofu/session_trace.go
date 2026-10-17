package main

import (
	"cmp"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/interface/cli"
	"tofu/internal/session"
)

const sessionTraceUsage = "tofu session trace <name|id> [--since 1h] [--from T] [--to T] [--agent NAME|lead] [--turn ID] [--kind K,K] [--grep TEXT] [--last N] [--wide] [--full] [--json]"

type traceKind string

const (
	kindTurn       traceKind = "turn"
	kindStop       traceKind = "stop"
	kindError      traceKind = "error"
	kindSpawn      traceKind = "spawn"
	kindReport     traceKind = "report"
	kindMessage    traceKind = "message"
	kindInsert     traceKind = "insert"
	kindCron       traceKind = "cron"
	kindRequest    traceKind = "request"
	kindMemory     traceKind = "memory"
	kindCall       traceKind = "call"
	kindShell      traceKind = "shell"
	kindGate       traceKind = "gate"
	kindHook       traceKind = "hook"
	kindCache      traceKind = "cache"
	kindCompaction traceKind = "compaction"
	kindList       traceKind = "list"
	kindRead       traceKind = "read"
	kindStartup    traceKind = "startup"
	kindNotice     traceKind = "notice"
	kindAttachment traceKind = "attachment"
)

func traceKinds() []traceKind {
	return []traceKind{kindTurn, kindStop, kindError, kindSpawn, kindReport, kindMessage, kindInsert, kindCron, kindRequest, kindMemory,
		kindCall, kindShell, kindGate, kindHook, kindCache, kindCompaction, kindList, kindRead, kindStartup, kindNotice, kindAttachment}
}

type traceOutcome string

const (
	traceFailed  traceOutcome = "failed"
	traceStopped traceOutcome = "stopped"
)

const traceLead = "lead"

type traceRow struct {
	At         time.Time    `json:"at"`
	Kind       traceKind    `json:"kind"`
	Agent      string       `json:"agent,omitempty"`
	Turn       string       `json:"turn,omitempty"`
	ID         string       `json:"id,omitempty"`
	State      string       `json:"state,omitempty"`
	Outcome    traceOutcome `json:"outcome,omitempty"`
	Text       string       `json:"text"`
	RecordedIn string       `json:"recorded_in,omitempty"`
}

func (row traceRow) is(kind traceKind) bool {
	return row.Kind == kind || kind == kindError && row.Outcome == traceFailed || kind == kindStop && row.Outcome == traceStopped
}

func (row traceRow) who() string { return cmp.Or(row.Agent, traceLead) }

type traceFilter struct {
	From   time.Time   `json:"from,omitzero"`
	To     time.Time   `json:"to,omitzero"`
	Agents []string    `json:"agents,omitempty"`
	Turn   string      `json:"turn,omitempty"`
	Kinds  []traceKind `json:"kinds,omitempty"`
	Grep   string      `json:"grep,omitempty"`
	Last   int         `json:"last,omitempty"`
}

func (f traceFilter) empty() bool {
	return f.From.IsZero() && f.To.IsZero() && len(f.Agents) == 0 && f.Turn == "" && len(f.Kinds) == 0 && f.Grep == "" && f.Last == 0
}

func (f traceFilter) keeps(row traceRow) bool {
	return (f.From.IsZero() || !row.At.Before(f.From)) && (f.To.IsZero() || row.At.Before(f.To)) &&
		(len(f.Agents) == 0 || slices.Contains(f.Agents, row.who())) && (f.Turn == "" || strings.Contains(row.Turn, f.Turn)) &&
		(len(f.Kinds) == 0 || slices.ContainsFunc(f.Kinds, row.is)) &&
		(f.Grep == "" || strings.Contains(strings.ToLower(row.Text+" "+row.ID+" "+row.Agent+" "+row.Turn), f.Grep))
}

type traceAsk struct {
	handle     string
	filter     traceFilter
	full, wide bool
}

type traceTimeline struct {
	Session string      `json:"session"`
	Handle  string      `json:"handle"`
	Filter  traceFilter `json:"filter"`
	Events  int         `json:"events"`
	Matched int         `json:"matched"`
	Rows    []traceRow  `json:"rows"`

	began  time.Time
	agents []session.AgentRun
}

func sessionTraceVerb(o verbOutput, args []string, now time.Time) int {
	o.verb, o.usageLine = "session trace", sessionTraceUsage
	ask, err := sessionTraceArgs(args, now)
	if err != nil {
		return o.usage(err)
	}
	store, err := session.Open()
	if err != nil {
		return o.fail(err)
	}
	if ask.filter.empty() && (o.asJSON || ask.full) {
		report, err := sessionTrace(store, ask.handle)
		if err != nil {
			return o.fail(err)
		}
		return o.done(true, report, func(page cli.Page) []string { return sessionTraceLines(page, report) })
	}
	timeline, err := sessionTimeline(store, ask.handle, ask.filter)
	if err != nil {
		return o.fail(err)
	}
	if ask.filter.empty() {
		return o.done(true, timeline, func(page cli.Page) []string { return traceSummaryLines(page, timeline) })
	}
	timeline.Rows = slices.DeleteFunc(timeline.Rows, func(row traceRow) bool { return !ask.filter.keeps(row) })
	timeline.Matched = len(timeline.Rows)
	if ask.filter.Last > 0 && ask.filter.Last < len(timeline.Rows) {
		timeline.Rows = timeline.Rows[len(timeline.Rows)-ask.filter.Last:]
	}
	return o.done(true, timeline, func(page cli.Page) []string { return traceRowLines(page, timeline, ask.wide) })
}

func sessionTraceArgs(args []string, now time.Time) (traceAsk, error) {
	var ask traceAsk
	var handles []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == jsonFlag:
			continue
		case arg == "--full":
			ask.full = true
			continue
		case arg == "--wide":
			ask.wide = true
			continue
		case !strings.HasPrefix(arg, "-"):
			handles = append(handles, arg)
			continue
		case i+1 == len(args):
			return ask, fmt.Errorf("%s needs a value", arg)
		}
		i++
		value := args[i]
		var err error
		switch arg {
		case "--since", "--from":
			ask.filter.From, err = traceTime(value, now)
		case "--to", "--until":
			ask.filter.To, err = traceTime(value, now)
		case "--agent":
			ask.filter.Agents = strings.Split(value, ",")
		case "--turn":
			ask.filter.Turn = value
		case "--kind":
			for _, named := range strings.Split(value, ",") {
				if !slices.Contains(traceKinds(), traceKind(named)) {
					return ask, fmt.Errorf("--kind %q is none of %v", named, traceKinds())
				}
				ask.filter.Kinds = append(ask.filter.Kinds, traceKind(named))
			}
		case "--grep":
			ask.filter.Grep = strings.ToLower(value)
		case "--last":
			if ask.filter.Last, err = strconv.Atoi(value); err == nil && ask.filter.Last < 1 {
				err = fmt.Errorf("--last %s, want 1 or more", value)
			}
		default:
			return ask, fmt.Errorf("unknown argument %q", arg)
		}
		if err != nil {
			return ask, err
		}
	}
	if len(handles) != 1 {
		return ask, fmt.Errorf("%d operands, want <name|id>", len(handles))
	}
	if ask.full && !ask.filter.empty() {
		return ask, fmt.Errorf("--full prints every section and takes no filter")
	}
	ask.handle = handles[0]
	return ask, nil
}

func traceTime(value string, now time.Time) (time.Time, error) {
	if ago, err := time.ParseDuration(value); err == nil {
		return now.Add(-ago), nil
	}
	for _, layout := range []string{time.RFC3339, time.DateTime, "2006-01-02 15:04", "2006-01-02T15:04:05", "2006-01-02T15:04", time.DateOnly} {
		if at, err := time.ParseInLocation(layout, value, now.Location()); err == nil {
			return at, nil
		}
	}
	for _, layout := range []string{time.TimeOnly, "15:04"} {
		clock, err := time.ParseInLocation(layout, value, now.Location())
		if err != nil {
			continue
		}
		year, month, day := now.Date()
		at := time.Date(year, month, day, clock.Hour(), clock.Minute(), clock.Second(), 0, now.Location())
		if at.After(now) {
			at = at.AddDate(0, 0, -1)
		}
		return at, nil
	}
	return time.Time{}, fmt.Errorf("%q is not a time: give 1h, 04:10, 2026-10-17 04:10 or 2026-10-17T04:10:00-03:00", value)
}

func sessionTimeline(store *session.Store, handle string, filter traceFilter) (traceTimeline, error) {
	store = store.ReadOnce()
	header, err := sessionHeader(store, handle)
	if err != nil {
		return traceTimeline{}, err
	}
	events, err := store.Events(header.ID)
	if err != nil {
		return traceTimeline{}, err
	}
	exchanges, err := store.Exchanges(header.ID)
	if err != nil {
		return traceTimeline{}, err
	}
	ancestors, err := store.Ancestors(header.ID)
	if err != nil {
		return traceTimeline{}, err
	}
	if !filter.To.IsZero() && !filter.To.After(header.At) {
		ancestors = nil
	}
	carried := func(event session.Event) bool { return header.CarriedFrom != nil && event.Turn == header.ID }
	rows := traceRows(slices.DeleteFunc(slices.Clone(events), carried), exchanges)
	timeline := traceTimeline{Session: header.ID, Handle: handleOf(store, header.ID), Filter: filter, Events: len(events), began: header.At, agents: slices.Clone(header.Agents)}
	outsideHere := func(event session.Event) bool {
		return event.Agent == "" || event.At.Before(header.At) || header.EndedAt != nil && !event.At.Before(*header.EndedAt)
	}
	for _, from := range ancestors {
		stillRunning := slices.ContainsFunc(from.Agents, func(run session.AgentRun) bool { return run.EndedAt == nil || run.EndedAt.After(header.At) })
		info, err := os.Stat(store.EventsPath(from.ID))
		if !stillRunning || err != nil || info.ModTime().Before(header.At) {
			continue
		}
		events, err := store.Events(from.ID)
		if err != nil {
			return traceTimeline{}, err
		}
		borrowed, there := traceRows(slices.DeleteFunc(slices.Clone(events), outsideHere), nil), handleOf(store, from.ID)
		for i := range borrowed {
			borrowed[i].RecordedIn = there
		}
		for _, run := range from.Agents {
			known := slices.ContainsFunc(timeline.agents, func(seen session.AgentRun) bool { return seen.Agent == run.Agent })
			if !known && slices.ContainsFunc(borrowed, func(row traceRow) bool { return row.Agent == run.Agent }) {
				timeline.agents = append(timeline.agents, run)
			}
		}
		rows = append(rows, borrowed...)
	}
	slices.SortStableFunc(rows, func(a, b traceRow) int { return a.At.Compare(b.At) })
	timeline.Rows, timeline.Matched = rows, len(rows)
	return timeline, nil
}
