package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"tofu/interface/cli"
	"tofu/internal/boardy"
)

const boardyRuleUsage = "tofu boardy rule list | show|remove <id> | add|edit <id> [--file hook.yaml] [--when before|after] [--on event] [--every 1h|--daily 09:00] [--do \"verb arg\"] [--then refuse|nudge|ask|record] [--text t] [--from s] [--to s] [--type t] [--priority p] [--labels a,b] [--actor a] [--epic E|set|empty] [--sprint S|set|empty] [--owns glob] [--in-status-for 2h] | test <id> [ticket] [--to status] [--board KEY] [--scope board|project|user] [--as name] [--json]"

func (c boardyCall) rule() int {
	if len(c.args) == 0 || len(c.args) == 1 && c.args[0] != "list" {
		return c.o.usage(errors.New("name list, or show, add, edit, remove or test and a hook id"))
	}
	request := boardy.RuleRequest{Board: c.flags["board"], Scope: boardy.HookScope(c.flags["scope"])}
	if len(c.args) > 1 {
		request.ID = c.args[1]
	}
	switch c.args[0] {
	case "list":
		return c.ruleList()
	case "show":
		answer, err := c.api().RuleGet(request)
		return c.hookShown(answer, err, cli.Idle, "")
	case "add", "edit":
		return c.rulePut(request, c.args[0] == "edit")
	case "remove":
		answer, err := c.api().RuleRemove(request)
		if err != nil {
			return c.o.fail(err)
		}
		return c.o.done(true, answer, func(page cli.Page) []string {
			return []string{page.Receipt(cli.Removed, "removed "+answer.Hook.ID+" from "+string(answer.Hook.Scope)+" scope", answer.Hook.File)}
		})
	case "test":
		return c.ruleTest(request)
	}
	return c.o.usage(fmt.Errorf("%q is not list, show, add, edit, remove or test", c.args[0]))
}

func (c boardyCall) ruleList() int {
	answer, err := c.api().Rules(boardy.RulesRequest{Board: c.flags["board"]})
	if err != nil {
		return c.o.fail(err)
	}
	return c.o.done(true, answer, func(page cli.Page) []string {
		lines := page.Title(answer.Board+" hooks", []string{plural(len(answer.Hooks), "hook")}, cli.Verdict{})
		for _, invalid := range answer.Invalid {
			lines = append(lines, page.Receipt(cli.Warn, "not loaded: "+invalid, ""))
		}
		if len(answer.Hooks) == 0 {
			return append(lines, page.Label("no hooks: tofu boardy rule add <id> --when before --on ticket.move --do \"field epic\" --then refuse"))
		}
		rows := []cli.Row{{Cells: []string{"hook", "scope", "last fired", "file"}}}
		for _, row := range answer.Hooks {
			rows = append(rows, cli.Row{Mark: cli.Idle, Cells: []string{row.ID, string(row.Scope), fired(row.LastFired), filepath.Base(row.File)}})
		}
		for at, line := range page.Rows(rows) {
			lines = append(lines, line)
			if at > 0 {
				lines = append(lines, cli.Indent(page.Label(answer.Hooks[at-1].Trigger+" · "+does(answer.Hooks[at-1])))...)
			}
		}
		return lines
	})
}

func (c boardyCall) rulePut(request boardy.RuleRequest, edit bool) int {
	existing, err := c.api().RuleGet(request)
	base, mark, verb := "", cli.Added, "added "
	switch {
	case edit && err != nil:
		return c.o.fail(err)
	case edit:
		base, request.Scope, mark, verb = existing.Source, existing.Hook.Scope, cli.Changed, "changed "
	case err == nil:
		return c.o.fail(fmt.Errorf("hook %s exists: tofu boardy rule edit %s", request.ID, request.ID))
	}
	if file := c.flags["file"]; file != "" {
		raw, err := os.ReadFile(file)
		if err != nil {
			return c.o.fail(err)
		}
		base = string(raw)
	}
	answer, err := c.api().RulePut(boardy.RulePutRequest{Board: request.Board, Scope: request.Scope, Source: hookSource(base, request.ID, c.flags)})
	return c.hookShown(answer, err, mark, verb)
}

func (c boardyCall) ruleTest(request boardy.RuleRequest) int {
	test := boardy.RuleTestRequest{Board: request.Board, Scope: request.Scope, ID: request.ID, To: boardy.Status(c.flags["to"])}
	if len(c.args) > 2 {
		test.Ticket = c.args[2]
	}
	answer, err := c.api().RuleTest(test)
	if err != nil {
		return c.o.fail(err)
	}
	mark := cli.Done
	switch {
	case !answer.Holds:
		mark = cli.Idle
	case answer.Blocks:
		mark = cli.Fail
	case answer.Fails:
		mark = cli.Warn
	}
	return c.o.done(true, answer, func(page cli.Page) []string {
		lines := page.Title("test "+answer.Hook.ID+" on "+dash(answer.Ticket)+" to "+dash(string(answer.To)), []string{answer.Hook.Trigger, does(answer.Hook)}, cli.Verdict{Mark: mark, Text: answer.Verdict})
		if answer.Detail != "" {
			lines = append(lines, cli.Indent(strings.Split(answer.Detail, "\n")...)...)
		}
		return append(lines, page.Label("nothing was written"))
	})
}

func (c boardyCall) hookShown(answer boardy.RuleAnswer, err error, mark cli.Mark, verb string) int {
	if err != nil {
		return c.o.fail(err)
	}
	row := answer.Hook
	return c.o.done(true, answer, func(page cli.Page) []string {
		lines := page.Title(verb+"hook "+row.ID, []string{string(row.Scope), answer.Board}, cli.Verdict{Mark: mark})
		lines = append(lines, page.Facts([]cli.Fact{
			{Label: "trigger", Text: row.Trigger},
			{Label: "does", Text: does(row)},
			{Label: "text", Text: dash(row.Text)},
			{Label: "last fired", Text: fired(row.LastFired)},
			{Label: "file", Text: page.Path(row.File)},
		})...)
		return append(append(lines, ""), cli.Indent(strings.Split(strings.TrimSpace(answer.Source), "\n")...)...)
	})
}

func does(row boardy.HookRow) string {
	text := strings.TrimSpace(string(row.Do) + " " + row.Arg)
	if row.Then != "" {
		text += ", then " + string(row.Then)
	}
	return text
}

func fired(at time.Time) string {
	if at.IsZero() {
		return "never"
	}
	return at.Local().Format(time.DateTime)
}

func hookSource(base, id string, flags map[string]string) string {
	top := []string{"id", "when", "on", "every", "daily", "do", "then", "text"}
	under := []string{"from", "to", "type", "priority", "labels", "actor", "epic", "sprint", "owns", "in_status_for"}
	values := map[string]string{}
	var unknown []string
	for line := range strings.Lines(base) {
		key, value, found := strings.Cut(strings.TrimSpace(line), ":")
		if key = strings.TrimSpace(key); !found || key == "if" {
			continue
		}
		if !slices.Contains(top, key) && !slices.Contains(under, key) {
			unknown = append(unknown, key)
		}
		values[key] = strings.Trim(strings.TrimSpace(value), `"`)
	}
	for name, value := range flags {
		values[strings.ReplaceAll(name, "-", "_")] = value
	}
	values["id"] = id
	var lines, conditions []string
	for _, key := range slices.Concat(top, unknown) {
		if values[key] != "" {
			lines = append(lines, key+": "+values[key])
		}
	}
	for _, key := range under {
		if values[key] != "" {
			conditions = append(conditions, "  "+key+": "+values[key])
		}
	}
	if conditions != nil {
		lines = append(append(lines, "if:"), conditions...)
	}
	return strings.Join(lines, "\n") + "\n"
}
