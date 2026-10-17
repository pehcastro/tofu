package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"tofu/interface/cli"
	"tofu/internal/boardy"
	"tofu/internal/widget"
)

func (c boardyCall) audit() int {
	if len(c.args) > 0 {
		return c.o.usage(fmt.Errorf("unexpected %q", c.args[0]))
	}
	answer, err := c.api().Audit(boardy.AuditRequest{Board: c.flags["board"]})
	if err != nil {
		return c.o.fail(err)
	}
	return c.o.done(true, answer, func(page cli.Page) []string { return c.auditLines(page, answer.Audit) })
}

func (c boardyCall) auditLines(page cli.Page, audit boardy.Audit) []string {
	listed := func(head string, items []string) string {
		if len(items) == 0 {
			return head
		}
		return head + ": " + strings.Join(items, ", ")
	}
	aged := func(tickets []boardy.Aging) string {
		items := make([]string, len(tickets))
		for i, one := range tickets {
			items[i] = one.ID + " " + strconv.Itoa(one.Days) + "d"
		}
		return listed(strconv.Itoa(len(tickets)), items)
	}
	var facts []cli.Fact
	for _, bucket := range audit.Review {
		label := fmt.Sprintf("review %dd to %dd", bucket.FromDays, bucket.ToDays)
		switch {
		case bucket.FromDays == 0:
			label = fmt.Sprintf("review under %dd", bucket.ToDays)
		case bucket.ToDays == 0:
			label = fmt.Sprintf("review %dd or more", bucket.FromDays)
		}
		facts = append(facts, cli.Fact{Label: label, Text: listed(strconv.Itoa(len(bucket.Tickets)), idRuns(bucket.Tickets))})
	}
	unacted := make([]string, len(audit.Unacted))
	for i, report := range audit.Unacted {
		unacted[i] = report.Ticket + " " + report.Agent + " " + report.At.Local().Format(time.DateTime)
	}
	duplicates := make([]string, len(audit.Duplicates))
	for i, pair := range audit.Duplicates {
		duplicates[i] = fmt.Sprintf("%s %s %.2f", pair.Tickets[0], pair.Tickets[1], pair.Overlap)
	}
	slowest := make([]string, len(audit.Lag.Slowest))
	for i, slow := range audit.Lag.Slowest {
		slowest[i] = slow.Ticket + " " + minutes(slow.Minutes)
	}
	facts = append(facts,
		cli.Fact{Label: "reports not acted on", Text: listed(strconv.Itoa(len(unacted)), unacted)},
		cli.Fact{Label: "no epic or sprint", Text: listed(fmt.Sprintf("%d of %d live", len(audit.Unplanned), audit.Live), idRuns(audit.Unplanned))},
		cli.Fact{Label: "likely duplicates", Text: listed(strconv.Itoa(len(duplicates))+" by title word overlap, a cheap local guess", duplicates)},
		cli.Fact{Label: "blocked, no question", Text: aged(audit.Unasked)},
		cli.Fact{Label: "stale todo", Text: aged(audit.StaleTodo)},
		cli.Fact{Label: "lead lag", Text: fmt.Sprintf("%d reports to a move, median %s, p%d %s", audit.Lag.Reports, minutes(audit.Lag.Median), audit.Lag.Percentile, minutes(audit.Lag.Slow))},
		cli.Fact{Label: "slowest moves", Text: strings.Join(slowest, ", ")},
	)
	return append(page.Title(audit.Board+" audit", []string{plural(audit.Tickets, c.words.Ticket), strconv.Itoa(audit.Live) + " live"}, cli.Verdict{}), page.Facts(facts)...)
}

func minutes(count int) string { return widget.Until(time.Duration(count) * time.Minute) }

func idRuns(ids []string) []string {
	var runs []string
	for at := 0; at < len(ids); {
		first, _ := boardy.SplitID(ids[at])
		end := at
		for end+1 < len(ids) {
			next, _ := boardy.SplitID(ids[end+1])
			if next.Child != 0 || first.Child != 0 || next.Key != first.Key || next.Number != first.Number+end+1-at {
				break
			}
			end++
		}
		run := ids[at]
		if end > at {
			run += ".." + strconv.Itoa(first.Number+end-at)
		}
		runs, at = append(runs, run), end+1
	}
	return runs
}
