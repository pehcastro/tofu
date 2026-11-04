package boardy

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"tofu/apps/cli/output"
	cli "tofu/apps/cli/page"
	"tofu/internal/konst"
	"tofu/internal/project/boardy"
)

func boardyDecideUsage(subcommand string) string {
	switch subcommand {
	case "ask":
		return "tofu boardy ask <ticket> --tldr text --eli5 text --options \"one | two\" --recommend n --why text [--as name] [--json]"
	case "catchup":
		return "tofu boardy catchup [--board KEY] [--answers] [--as name] [--json]"
	}
	return ""
}

func (c boardyCall) ask() int {
	if len(c.args) != 1 {
		return c.o.Usage(errors.New("name one " + c.words.Ticket))
	}
	recommend, err := strconv.Atoi(c.flags["recommend"])
	if err != nil {
		return c.o.Usage(errors.New("--recommend is the number of the option you would pick"))
	}
	q := boardy.Question{TLDR: c.flags["tldr"], ELI5: c.flags["eli5"], Options: strings.Split(c.flags["options"], "|"), Recommend: recommend, Why: c.flags["why"]}
	ticket, err := c.managed.Ask(c.args[0], q, c.actor)
	return c.receipt(ticket, err, cli.Added, "asked on "+ticket.ID+": "+strings.TrimSpace(q.TLDR))
}

func (c boardyCall) catchup() int {
	key, err := c.board()
	if err != nil {
		return c.o.Fail(err)
	}
	var caught boardy.Catchup
	if c.flags["answers"] != "" {
		caught, err = c.managed.Answers(key, c.actor)
	} else {
		caught, err = c.managed.Catchup(key)
	}
	if err != nil {
		return c.o.Fail(err)
	}
	return c.o.Done(true, caught, func(page cli.Page) []string {
		facts := []string{output.Plural(len(caught.Open), "open question")}
		if len(caught.Bypasses) > 0 {
			facts = append(facts, bypassCount(len(caught.Bypasses)))
		}
		if c.flags["answers"] != "" {
			facts = append(facts, output.Plural(len(caught.Applied), "answer")+" read")
		}
		lines := page.Title(key+" catch up", facts, cli.Verdict{})
		for _, done := range caught.Applied {
			text := done.Ticket + " Q" + strconv.Itoa(done.Question) + " answered: " + done.Logged
			if done.Back != "" {
				text += ", back to " + string(done.Back)
			}
			lines = append(lines, page.Receipt(cli.Done, text, ""))
		}
		for _, stale := range caught.Stale {
			lines = append(lines, page.Receipt(cli.Warn, stale.Ticket+" Q"+strconv.Itoa(stale.Question)+" no longer reads \""+stale.TLDR+"\", so the answer was not used: "+stale.Answer, ""))
		}
		for _, open := range caught.Open {
			q := open.Question
			lines = append(lines, "", page.Subject(open.Ticket+" Q"+strconv.Itoa(q.ID)+" "+q.TLDR))
			lines = append(lines, cli.Indent(page.Label(open.Title+" ("+string(open.Status)+")"))...)
			lines = append(lines, cli.Indent(strings.Split(q.ELI5, "\n")...)...)
			rows := make([]cli.Row, len(q.Options))
			for i, option := range q.Options {
				rows[i] = cli.Row{Mark: cli.Idle, Cells: []string{strconv.Itoa(i+1) + ".", option}}
			}
			if q.Recommend >= 1 && q.Recommend <= len(rows) {
				rows[q.Recommend-1].Mark, rows[q.Recommend-1].Hint = cli.Active, "recommended: "+q.Why
			}
			lines = append(lines, cli.Indent(page.Rows(rows)...)...)
			if open.Answer != "" {
				lines = append(lines, cli.Indent(page.Label("answer: "+open.Answer))...)
			}
		}
		if len(caught.Bypasses) > 0 {
			lines = append(lines, "", page.Subject("Bypasses for you"))
		}
		for _, r := range caught.Bypasses {
			lines = append(lines, cli.Indent(page.Rows([]cli.Row{{Mark: cli.Active, Cells: []string{r.Line()}}})...)...)
		}
		for _, f := range caught.Flagged {
			lines = append(lines, page.Receipt(cli.Warn, fmt.Sprintf("%s is bypassed %d times in %d days: fix the hook or the work", f.Hook, f.Times, konst.BoardBypassFlagDays), ""))
		}
		hint := "answer in the file, then tofu boardy catchup --answers"
		if len(caught.Open) == 0 && len(caught.Bypasses) > 0 {
			hint = "answer a bypass with tofu boardy bypass answer <B> yes|no"
		} else if len(caught.Open) == 0 {
			hint = "nothing waits on a decision"
		}
		return append(lines, "", page.Receipt(cli.Idle, hint, caught.File))
	})
}
