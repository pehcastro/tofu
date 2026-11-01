package main

import (
	"cmp"
	"errors"
	"strings"
	"time"

	"tofu/interface/cli"
	"tofu/internal/session"
)

const sessionWorkUsage = "tofu session work <name|id> --ticket ID [--json]"

const (
	workBrief  = "brief"
	workMove   = "move"
	workReport = "report"
)

type workStep struct{ what, said string }

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
	timeline, err := sessionTimeline(store, handles[0], traceFilter{})
	if err != nil {
		return o.fail(err)
	}
	work := sessionWork{Session: timeline.Session, Handle: timeline.Handle, Ticket: ticket, Rows: []workRow{}}
	for _, row := range timeline.Rows {
		if row.Ticket == ticket {
			work.Rows = append(work.Rows, workRow{At: row.At, What: cmp.Or(row.work.what, string(row.Kind)), Agent: row.who(), State: row.State, Text: cmp.Or(row.work.said, row.Text)})
		}
	}
	return o.done(true, work, func(page cli.Page) []string { return sessionWorkLines(page, work) })
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
		lines = append(append(lines, cli.Indent(head)...), cli.Indent(cli.Indent(rest[i]...)...)...)
	}
	return lines
}
