package main

import (
	"cmp"
	"errors"
	"slices"
	"strings"
	"time"

	"tofu/internal/cli/page"
	"tofu/internal/project/boardy"
	"tofu/internal/store/session"
)

const sessionWorkUsage = "tofu session work <name|id|head> --ticket ID [--json]"

const (
	workBrief  = "brief"
	workMove   = "move"
	workReport = "report"
	workBoard  = "board"
)

type workStep struct{ what, said, to string }

type workRow struct {
	At    time.Time `json:"at"`
	What  string    `json:"what"`
	Agent string    `json:"agent"`
	State string    `json:"state,omitempty"`
	Text  string    `json:"text"`
}

type sessionWork struct {
	Session string    `json:"session"`
	Handle  string    `json:"handle"`
	Ticket  string    `json:"ticket"`
	Rows    []workRow `json:"rows"`
}

func sessionWorkVerb(o verbOutput, args []string) int {
	o.verb, o.usageLine = "session work", sessionWorkUsage
	var handles []string
	ticket := ""
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == jsonFlag:
		case args[i] == "--ticket" && i+1 == len(args):
			return o.usage(errors.New("--ticket needs a value"))
		case args[i] == "--ticket":
			i++
			ticket = args[i]
		case strings.HasPrefix(args[i], "-"):
			return o.usage(errors.New("unknown argument " + args[i]))
		default:
			handles = append(handles, args[i])
		}
	}
	if len(handles) != 1 || ticket == "" {
		return o.usage(errors.New("want one <name|id> and --ticket ID"))
	}
	store, err := session.Open()
	if err != nil {
		return o.fail(err)
	}
	store = store.ReadOnce()
	header, err := sessionHeader(store, handles[0])
	if err != nil {
		return o.fail(err)
	}
	ancestors, err := store.Ancestors(header.ID)
	if err != nil {
		return o.fail(err)
	}
	lineage := append([]session.Header{header}, ancestors...)
	var events []session.Event
	for _, generation := range slices.Backward(lineage) {
		logged, err := store.Events(generation.ID)
		if err != nil {
			return o.fail(err)
		}
		events = append(events, slices.DeleteFunc(slices.Clone(logged), func(event session.Event) bool {
			return generation.CarriedFrom != nil && event.Turn == generation.ID
		})...)
	}
	work := sessionWork{Session: header.ID, Handle: handleOf(store, header.ID), Ticket: ticket, Rows: []workRow{}}
	toolMoved := map[string]int{}
	for _, row := range traceRows(events, nil) {
		if row.Ticket != ticket {
			continue
		}
		if row.work.what == workMove && row.State == session.ToolOutcomeRan {
			toolMoved[row.who()+" to "+row.work.to]++
		}
		work.Rows = append(work.Rows, workRow{At: row.At, What: cmp.Or(row.work.what, string(row.Kind)), Agent: row.who(), State: row.State, Text: cmp.Or(row.work.said, row.Text)})
	}
	moves, err := boardMoves(store.Project(), ticket, lineage)
	if err != nil {
		return o.fail(err)
	}
	for _, move := range moves {
		who := cmp.Or(move.Agent, traceLead)
		if move.Author.Kind == boardy.AuthorHook {
			who = move.Author.Name
		}
		if key := who + " to " + string(move.To); toolMoved[key] > 0 {
			toolMoved[key]--
			continue
		}
		work.Rows = append(work.Rows, workRow{At: move.At.Local(), What: workBoard, Agent: who, State: string(boardy.Moved), Text: traced(string(move.From)+" to "+string(move.To), move.Text)})
	}
	slices.SortStableFunc(work.Rows, func(a, b workRow) int { return a.At.Compare(b.At) })
	return o.done(true, work, func(page cli.Page) []string { return sessionWorkLines(page, work) })
}

func boardMoves(project, ticket string, lineage []session.Header) ([]boardy.Event, error) {
	ref, err := boardy.SplitID(ticket)
	if err != nil {
		return nil, nil
	}
	board, err := boardy.OpenStore(project)
	if err != nil {
		return nil, err
	}
	began, ended := lineage[len(lineage)-1].At.Truncate(time.Second), lineage[0].EndedAt
	events, err := board.Events(ref.Key, 0)
	return slices.DeleteFunc(events, func(event boardy.Event) bool {
		if event.Ticket != ticket || event.Kind != boardy.Moved || event.Author == nil {
			return true
		}
		if event.Author.Kind == boardy.AuthorHook {
			return event.At.Before(began) || ended != nil && event.At.After(*ended)
		}
		ranIn := cmp.Or(event.Session, event.Author.Session)
		return !slices.ContainsFunc(lineage, func(generation session.Header) bool { return generation.ID == ranIn })
	}), err
}

func sessionWorkLines(page cli.Page, work sessionWork) []string {
	lines := append(page.Title("Work", []string{work.Handle, work.Ticket, plural(len(work.Rows), "row")}, cli.Verdict{}), "")
	if len(work.Rows) == 0 {
		return append(lines, cli.Indent(page.Label("no row carries "+work.Ticket))...)
	}
	heads, rest := make([]cli.Row, len(work.Rows)), make([][]string, len(work.Rows))
	for i, row := range work.Rows {
		said := strings.Split(strings.TrimSpace(strings.ReplaceAll(row.Text, "\r\n", "\n")), "\n")
		heads[i], rest[i] = cli.Row{Mark: cli.Idle, Cells: []string{row.At.Format(time.TimeOnly), row.What, row.Agent, row.State}, Detail: said[0]}, said[1:]
	}
	for i, head := range page.Rows(heads) {
		lines = append(lines, cli.Indent(head)...)
		for _, said := range cli.Indent(cli.Indent(rest[i]...)...) {
			lines = append(lines, strings.TrimRight(said, " \t"))
		}
	}
	return lines
}
