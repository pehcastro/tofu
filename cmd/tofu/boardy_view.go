package main

import (
	"cmp"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"tofu/interface/cli"
	"tofu/internal/boardy"
)

const boardyReportListed = 5

func boardyViewUsageOf(subcommand string) string {
	switch subcommand {
	case "view":
		return "tofu boardy view [name] [--board KEY] [--epic E] [--label l] [--assignee name] [--as name] [--json]"
	case "report":
		return "tofu boardy report [--board KEY] [--since YYYY-MM-DD|Nd] [--json]"
	case "epic":
		return "tofu boardy epic list|new <ID> <title> [--milestone M]|milestone <ID> <title> [--due YYYY-MM-DD|Nd]|done <ID> [--board KEY] [--json]"
	case "sprint":
		return "tofu boardy sprint list|new <ID> <title> [--start YYYY-MM-DD|Nd] [--end YYYY-MM-DD|Nd]|start <ID>|close <ID> [--board KEY] [--json]"
	}
	return ""
}

func (c boardyCall) api() boardy.API {
	return boardy.API{Board: c.managed, Actor: c.actor, Words: c.words}
}

func (c boardyCall) view() int {
	if len(c.args) > 1 {
		return c.o.usage(fmt.Errorf("unexpected %q", c.args[1]))
	}
	request := boardy.ListRequest{Board: c.flags["board"], Filter: boardy.Filter{Epic: c.flags["epic"], Label: c.flags["label"], Assignee: c.flags["assignee"]}}
	if len(c.args) == 1 {
		request.View = c.args[0]
	}
	answer, err := c.api().List(request)
	if err != nil {
		return c.o.fail(err)
	}
	return c.o.done(true, answer, func(page cli.Page) []string {
		count := 0
		for _, group := range answer.Groups {
			count += len(group.Tickets)
		}
		sprint := "no active " + c.words.Sprint
		if answer.Sprint != "" {
			sprint = c.words.Sprint + " " + answer.Sprint
		}
		lines := page.Title(answer.Board+" "+answer.View.Name, []string{sprint, plural(count, c.words.Ticket)}, cli.Verdict{})
		if count == 0 {
			return append(lines, page.Label("no "+c.words.Ticket+" in this view: tofu boardy view all"))
		}
		for _, group := range answer.Groups {
			if group.Name != "" || answer.View.GroupBy != boardy.Ungrouped {
				lines = append(lines, "", page.Subject(dash(group.Name)+" "+strconv.Itoa(len(group.Tickets))))
			}
			rows := make([]cli.Row, 0, len(group.Tickets))
			for _, t := range group.Tickets {
				rows = append(rows, cli.Row{Mark: statusMark(t.Status), Cells: []string{t.ID, string(t.Status), string(t.Priority), strconv.Itoa(t.Points), dash(t.Epic), dash(t.Assignee), t.Title}})
			}
			lines = append(lines, cli.Indent(page.Rows(rows)...)...)
		}
		return lines
	})
}

type boardyDays int

const (
	daysAgo   boardyDays = -1
	daysAhead boardyDays = 1
)

func boardyDate(raw string, toward boardyDays) (time.Time, error) {
	if raw == "" {
		return time.Time{}, nil
	}
	if days, isDays := strings.CutSuffix(raw, "d"); isDays {
		count, err := strconv.Atoi(days)
		return time.Now().AddDate(0, 0, int(toward)*count), err
	}
	return time.ParseInLocation(time.DateOnly, raw, time.Local)
}

func (c boardyCall) report() int {
	if len(c.args) > 0 {
		return c.o.usage(fmt.Errorf("unexpected %q", c.args[0]))
	}
	since, err := boardyDate(c.flags["since"], daysAgo)
	if err != nil {
		return c.o.usage(fmt.Errorf("--since %q is not a date or a number of days like 7d", c.flags["since"]))
	}
	answer, err := c.api().Report(boardy.ReportRequest{Board: c.flags["board"], Since: since})
	if err != nil {
		return c.o.fail(err)
	}
	return c.o.done(true, answer, func(page cli.Page) []string { return c.reportLines(page, answer.Report) })
}

func (c boardyCall) reportLines(page cli.Page, report boardy.Report) []string {
	window := "all time"
	if !report.Since.IsZero() {
		window = "since " + report.Since.Local().Format(time.DateOnly)
	}
	total := 0
	for _, status := range report.Statuses {
		total += status.Tickets
	}
	lines := page.Title(report.Board+" report", []string{window, plural(total, c.words.Ticket)}, cli.Verdict{})
	points := func(tally boardy.Tally) string {
		return fmt.Sprintf("%d of %d done, %d of %d %s", tally.Done, tally.Tickets, tally.Settled, tally.Points, c.words.Points)
	}
	sprint := cli.Fact{Label: c.words.Sprint, Text: "none active"}
	if report.Sprint != nil {
		sprint.Text = report.Sprint.Name + " " + report.Sprint.Title + ": " + points(*report.Sprint)
	}
	lines = append(lines, page.Facts([]cli.Fact{
		sprint,
		{Label: "moves", Text: strconv.Itoa(report.Moves)},
		{Label: "finished", Text: plural(len(report.Finished), c.words.Ticket)},
		{Label: "rounds", Text: strconv.Itoa(report.Rounds)},
		{Label: "in doing", Text: (time.Duration(report.Minutes) * time.Minute).String()},
	})...)
	var statuses []cli.Row
	for _, status := range report.Statuses {
		if status.Tickets > 0 {
			statuses = append(statuses, cli.Row{Mark: statusMark(boardy.Status(status.Name)), Cells: []string{status.Name, plural(status.Tickets, c.words.Ticket), strconv.Itoa(status.Points) + " " + c.words.Points}})
		}
	}
	lines = append(append(lines, "", page.Subject("status")), cli.Indent(page.Rows(statuses)...)...)
	if len(report.Epics) > 0 {
		epics := make([]cli.Row, 0, len(report.Epics))
		for _, epic := range report.Epics {
			epics = append(epics, cli.Row{Mark: cli.Idle, Cells: []string{epic.Name, epic.Title, points(epic)}})
		}
		lines = append(append(lines, "", page.Subject(c.words.Epic)), cli.Indent(page.Rows(epics)...)...)
	}
	waiting := func(heading string, mark cli.Mark, aging []boardy.Aging) {
		if len(aging) == 0 {
			return
		}
		rows := make([]cli.Row, 0, boardyReportListed)
		for _, one := range aging[:min(len(aging), boardyReportListed)] {
			rows = append(rows, cli.Row{Mark: mark, Cells: []string{one.ID, strconv.Itoa(one.Days) + "d", one.Title}, Detail: one.Reason})
		}
		if more := len(aging) - len(rows); more > 0 {
			rows = append(rows, cli.Row{Mark: cli.Idle, Cells: []string{"", "", strconv.Itoa(more) + " more: tofu boardy view " + heading}})
		}
		lines = append(append(lines, "", page.Subject(heading+" "+strconv.Itoa(len(aging)))), cli.Indent(page.Rows(rows)...)...)
	}
	waiting("review", cli.Active, report.Review)
	waiting("blocked", cli.Warn, report.Blocked)
	return lines
}

func (c boardyCall) epic() int {
	key, err := c.board()
	if err != nil {
		return c.o.fail(err)
	}
	words := c.words
	switch {
	case len(c.args) == 1 && c.args[0] == "list":
		epics, err := c.store.Epics(key)
		milestones, milestonesErr := c.store.Milestones(key)
		if err = errors.Join(err, milestonesErr); err != nil {
			return c.o.fail(err)
		}
		due := map[string]string{}
		for _, milestone := range milestones {
			due[milestone.ID] = milestone.ID + " " + milestone.Title
			if !milestone.Due.IsZero() {
				due[milestone.ID] += " due " + milestone.Due.Format(time.DateOnly)
			}
		}
		return c.o.done(true, struct {
			Epics      []boardy.Epic      `json:"epics"`
			Milestones []boardy.Milestone `json:"milestones"`
		}{epics, milestones}, func(page cli.Page) []string {
			rows := make([]cli.Row, 0, len(epics))
			for _, epic := range epics {
				mark := cli.Idle
				if epic.Done {
					mark = cli.Done
				}
				rows = append(rows, cli.Row{Mark: mark, Cells: []string{epic.ID, epic.Title, dash(cmp.Or(due[epic.Milestone], epic.Milestone))}})
			}
			return append(page.Title(key+" "+words.Epic+"s", []string{strconv.Itoa(len(epics))}, cli.Verdict{}), page.Rows(rows)...)
		})
	case len(c.args) >= 3 && c.args[0] == "new":
		epic := boardy.Epic{ID: c.args[1], Title: strings.Join(c.args[2:], " "), Milestone: c.flags["milestone"]}
		return c.planSaved(key, words.Epic+" "+epic.ID, epic, func() error { return c.store.SaveEpic(key, epic) })
	case len(c.args) >= 3 && c.args[0] == "milestone":
		milestone := boardy.Milestone{ID: c.args[1], Title: strings.Join(c.args[2:], " ")}
		if milestone.Due, err = boardyDate(c.flags["due"], daysAhead); err != nil {
			return c.o.usage(err)
		}
		return c.planSaved(key, "milestone "+milestone.ID, milestone, func() error { return c.store.SaveMilestone(key, milestone) })
	case len(c.args) == 2 && c.args[0] == "done":
		epics, err := c.store.Epics(key)
		for _, epic := range epics {
			if epic.ID == c.args[1] {
				epic.Done = true
				return c.planSaved(key, words.Epic+" "+epic.ID+" done", epic, func() error { return errors.Join(err, c.store.SaveEpic(key, epic)) })
			}
		}
		return c.o.fail(errors.Join(err, fmt.Errorf("the %s %s has no %s %s", words.Board, key, words.Epic, c.args[1])))
	}
	return c.o.usage(errors.New("name list, new, milestone or done"))
}

func (c boardyCall) sprint() int {
	key, err := c.board()
	if err != nil {
		return c.o.fail(err)
	}
	words := c.words
	switch {
	case len(c.args) == 1 && c.args[0] == "list":
		sprints, err := c.store.Sprints(key)
		if err != nil {
			return c.o.fail(err)
		}
		return c.o.done(true, sprints, func(page cli.Page) []string {
			rows := make([]cli.Row, 0, len(sprints))
			for _, sprint := range sprints {
				mark := map[boardy.SprintState]cli.Mark{boardy.Planned: cli.Idle, boardy.Active: cli.Active, boardy.Closed: cli.Done}[sprint.State]
				rows = append(rows, cli.Row{Mark: mark, Cells: []string{sprint.ID, string(sprint.State), sprint.Title, sprintDays(sprint)}})
			}
			return append(page.Title(key+" "+words.Sprint+"s", []string{strconv.Itoa(len(sprints))}, cli.Verdict{}), page.Rows(rows)...)
		})
	case len(c.args) >= 3 && c.args[0] == "new":
		sprint := boardy.Sprint{ID: c.args[1], Title: strings.Join(c.args[2:], " "), State: boardy.Planned}
		var startErr, endErr error
		sprint.Start, startErr = boardyDate(c.flags["start"], daysAhead)
		sprint.End, endErr = boardyDate(c.flags["end"], daysAhead)
		if err := errors.Join(startErr, endErr); err != nil {
			return c.o.usage(err)
		}
		return c.planSaved(key, words.Sprint+" "+sprint.ID, sprint, func() error { return c.store.SaveSprint(key, sprint) })
	case len(c.args) == 2 && c.args[0] == "start":
		return c.planSaved(key, words.Sprint+" "+c.args[1]+" active", c.args[1], func() error { return c.store.SetSprint(key, c.args[1], boardy.Active) })
	case len(c.args) == 2 && c.args[0] == "close":
		return c.planSaved(key, words.Sprint+" "+c.args[1]+" closed", c.args[1], func() error { return c.store.SetSprint(key, c.args[1], boardy.Closed) })
	}
	return c.o.usage(errors.New("name list, new, start or close"))
}

func sprintDays(sprint boardy.Sprint) string {
	if sprint.Start.IsZero() {
		return "-"
	}
	text := sprint.Start.Format(time.DateOnly)
	if !sprint.End.IsZero() {
		text += " to " + sprint.End.Format(time.DateOnly)
	}
	return text
}

func (c boardyCall) planSaved(key, what string, data any, write func() error) int {
	err := c.managed.Manages(key, c.actor, "change its "+c.words.Epic+"s and "+c.words.Sprint+"s")
	if err == nil {
		err = write()
	}
	if err != nil {
		return c.o.fail(err)
	}
	return c.o.done(true, data, func(page cli.Page) []string {
		return []string{page.Receipt(cli.Changed, what, c.store.BoardDir(key))}
	})
}
