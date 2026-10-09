package main

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"strings"

	"tofu/interface/cli"
	"tofu/internal/boardy"
	"tofu/internal/rule"
)

const boardySessionEnv = "TOFU_SESSION"

func boardyManageUsage(subcommand string) string {
	switch subcommand {
	case "manager":
		return "tofu boardy manager add|remove|list [name] [--board KEY] [--as name] [--json]"
	case "triage":
		return "tofu boardy triage list|accept|drop [request] [--board KEY] [--reason text] [--as name] [--json]"
	case "hand":
		return "tofu boardy hand <ticket> <board> [--project dir] [--as name] [--json]"
	case "assign":
		return "tofu boardy assign <ticket> <session or name> [--as name] [--json]"
	case "widen":
		return "tofu boardy widen <ticket> <glob,glob> [--as name] [--json]"
	case "check":
		return "tofu boardy check create|move|widen|close <ticket> [--to status] [--as name] [--json]"
	}
	return ""
}

func boardyIdentity(as string) (actor, person string) {
	if session := os.Getenv(boardySessionEnv); session != "" {
		return session, ""
	}
	actor = cmp.Or(as, "person")
	return actor, actor
}

func (c boardyCall) manage(subcommand string) int {
	actor, person := boardyIdentity(c.flags["as"])
	m := boardy.Managed{Local: boardy.Local{Store: c.store}, Person: person}
	switch subcommand {
	case "manager":
		return c.managers(m, actor)
	case "triage":
		return c.triage(m, actor)
	case "hand":
		return c.hand(m, actor)
	case "assign":
		if len(c.args) != 2 {
			return c.o.usage(errors.New("name a " + c.words.Ticket + " and who it goes to"))
		}
		ticket, err := m.Assign(c.args[0], c.args[1], actor)
		return c.receipt(ticket, err, cli.Changed, ticket.ID+" is assigned to "+ticket.Assignee)
	case "widen":
		if len(c.args) != 2 {
			return c.o.usage(errors.New("name a " + c.words.Ticket + " and the globs to add"))
		}
		ticket, err := m.Widen(c.args[0], splitList(c.args[1]), actor)
		return c.receipt(ticket, err, cli.Changed, ticket.ID+" owns "+strings.Join(ticket.Owns, ", "))
	}
	return c.check(actor, person)
}

func (c boardyCall) managers(m boardy.Managed, actor string) int {
	key, err := c.board()
	if err != nil {
		return c.o.fail(err)
	}
	if len(c.args) == 0 {
		return c.o.usage(errors.New("add, remove or list"))
	}
	board, err := c.store.Board(key)
	switch {
	case c.args[0] == "list" && len(c.args) == 1:
	case len(c.args) != 2:
		return c.o.usage(errors.New("name one session id or the person's name"))
	case c.args[0] == "add":
		board, err = m.AddManager(key, c.args[1], actor)
	case c.args[0] == "remove":
		board, err = m.RemoveManager(key, c.args[1], actor)
	default:
		return c.o.usage(fmt.Errorf("%q is not add, remove or list", c.args[0]))
	}
	if err != nil {
		return c.o.fail(err)
	}
	return c.o.done(true, board, func(page cli.Page) []string {
		facts := []string{fmt.Sprintf("%d managers", len(board.Managers))}
		if len(board.Managers) == 0 {
			facts = []string{"no managers, so the person manages it"}
		}
		lines := page.Title(key+" managers", facts, cli.Verdict{})
		for _, name := range board.Managers {
			lines = append(lines, page.Rows([]cli.Row{{Mark: cli.Idle, Cells: []string{name}}})...)
		}
		return lines
	})
}

func (c boardyCall) triage(m boardy.Managed, actor string) int {
	key, err := c.board()
	if err != nil {
		return c.o.fail(err)
	}
	switch {
	case len(c.args) == 1 && c.args[0] == "list":
		requests, err := c.store.Requests(key)
		if err != nil {
			return c.o.fail(err)
		}
		return c.o.done(true, requests, func(page cli.Page) []string {
			lines := page.Title(key+" triage", []string{fmt.Sprintf("%d requests", len(requests))}, cli.Verdict{})
			for _, req := range requests {
				lines = append(lines, page.Rows([]cli.Row{{Mark: cli.Idle, Cells: []string{req.ID, req.By, req.Ticket.Title}}})...)
			}
			return lines
		})
	case len(c.args) == 2 && c.args[0] == "accept":
		ticket, err := m.Accept(key, c.args[1], actor)
		if err != nil {
			return c.o.fail(err)
		}
		path, _ := c.store.TicketPath(ticket.ID)
		return c.o.done(true, ticket, func(page cli.Page) []string {
			return []string{page.Receipt(cli.Added, c.args[1]+" is "+ticket.ID+" "+ticket.Title, path)}
		})
	case len(c.args) == 2 && c.args[0] == "drop":
		if err := m.Drop(key, c.args[1], c.flags["reason"], actor); err != nil {
			return c.o.fail(err)
		}
		return c.o.done(true, map[string]string{"dropped": c.args[1]}, func(page cli.Page) []string {
			return []string{page.Receipt(cli.Removed, "dropped "+c.args[1]+" from the triage of "+key, "")}
		})
	}
	return c.o.usage(errors.New("list, accept <request> or drop <request>"))
}

func (c boardyCall) hand(m boardy.Managed, actor string) int {
	if len(c.args) != 2 {
		return c.o.usage(errors.New("name a " + c.words.Ticket + " and the " + c.words.Board + " it goes to"))
	}
	to := c.store
	if project := c.flags["project"]; project != "" {
		var err error
		if to, err = boardy.OpenStore(project); err != nil {
			return c.o.fail(err)
		}
	}
	landed, err := m.Hand(c.args[0], c.args[1], to, actor)
	if err != nil {
		return c.o.fail(err)
	}
	path, _ := to.TicketPath(landed.ID)
	return c.o.done(true, landed, func(page cli.Page) []string {
		if landed.ID == "" {
			return []string{page.Receipt(cli.Added, c.args[0]+" handed: "+landed.Reason, "")}
		}
		return []string{page.Receipt(cli.Added, c.args[0]+" is now "+landed.ID+" "+landed.Title, path)}
	})
}

func (c boardyCall) check(actor, person string) int {
	if len(c.args) != 2 {
		return c.o.usage(errors.New("name an event and a " + c.words.Ticket))
	}
	event, err := rule.ParseEvent(c.args[0])
	if err != nil {
		return c.o.usage(err)
	}
	ticket, err := c.store.Get(c.args[1])
	if err != nil {
		return c.o.fail(err)
	}
	path, _ := c.store.TicketPath(ticket.ID)
	fired, err := c.store.Check(ticket.Board(), rule.BoardEvent{Event: event, Ticket: ticket.ID, Path: path, From: string(ticket.Status),
		To: cmp.Or(c.flags["to"], string(ticket.Status)), Actor: actor, ByPerson: person != "" && actor == person})
	if err != nil {
		return c.o.fail(err)
	}
	blocked := 0
	for _, f := range fired {
		if f.Blocked {
			blocked++
		}
	}
	return c.o.done(blocked == 0, fired, func(page cli.Page) []string {
		lines := page.Title("check "+string(event)+" "+ticket.ID, []string{ticket.Board()}, firesVerdict(len(fired), blocked))
		for _, f := range fired {
			mark := cli.Warn
			if f.Blocked {
				mark = cli.Fail
			}
			lines = append(lines, page.Rows([]cli.Row{{Mark: mark, Cells: []string{f.RuleID, string(f.Mode)}, Detail: f.Text()}})...)
		}
		return lines
	})
}
