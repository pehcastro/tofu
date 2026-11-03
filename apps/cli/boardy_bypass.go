package main

import (
	"errors"
	"fmt"

	"tofu/internal/cli/page"
	"tofu/internal/konst"
	"tofu/internal/project/boardy"
)

const boardyBypassUsage = "tofu boardy bypass list | answer <B> yes|no [--why text] [--board KEY] [--as name] [--json]"

func (c boardyCall) bypass() int {
	key, err := c.board()
	if err != nil {
		return c.o.Fail(err)
	}
	api := boardy.API{Board: c.managed, Actor: c.actor}
	switch {
	case len(c.args) == 1 && c.args[0] == "list":
		listed, err := api.BypassList(boardy.BypassListRequest{Board: key})
		if err != nil {
			return c.o.Fail(err)
		}
		return c.o.Done(true, listed, func(page cli.Page) []string {
			lines := page.Title(key+" bypasses", []string{bypassCount(len(listed.Bypasses))}, cli.Verdict{})
			for _, r := range listed.Bypasses {
				mark := cli.Idle
				if r.State == boardy.BypassOpen || r.State == boardy.BypassReview {
					mark = cli.Active
				}
				lines = append(lines, page.Rows([]cli.Row{{Mark: mark, Cells: []string{r.Line()}}})...)
			}
			for _, f := range listed.Flagged {
				lines = append(lines, page.Receipt(cli.Warn, fmt.Sprintf("%s is bypassed %d times in %d days: fix the hook or the work", f.Hook, f.Times, konst.BoardBypassFlagDays), ""))
			}
			if len(listed.Bypasses) == 0 {
				lines = append(lines, page.Label("no bypass asked"))
			}
			return lines
		})
	case len(c.args) == 3 && c.args[0] == "answer" && (c.args[2] == "yes" || c.args[2] == "no"):
		answered, err := api.BypassAnswer(boardy.BypassAnswerRequest{Board: key, ID: c.args[1], Yes: c.args[2] == "yes", Why: c.flags["why"]})
		if err != nil {
			return c.o.Fail(err)
		}
		text := answered.Bypass.ID + " answered " + string(answered.Bypass.State)
		if answered.Moved != nil {
			text += ", so " + answered.Moved.ID + " is " + string(answered.Moved.Status)
		}
		return c.o.Done(true, answered, func(page cli.Page) []string { return []string{page.Receipt(cli.Changed, text, "")} })
	}
	return c.o.Usage(errors.New("list, or answer <B> yes|no"))
}

func bypassCount(n int) string {
	if n == 1 {
		return "1 bypass"
	}
	return fmt.Sprintf("%d bypasses", n)
}
