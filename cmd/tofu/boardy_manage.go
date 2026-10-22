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
	"tofu/internal/subagent"
)

const (
	claudeCodeEnv        = "CLAUDECODE"
	claudeCodeSessionEnv = "CLAUDE_CODE_SESSION_ID"
	codexThreadEnv       = "CODEX_THREAD_ID"
)

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

func boardyIdentity(as string) (actor, person, lead string) {
	if session := os.Getenv(subagent.SessionEnv); session != "" {
		return session, "", ""
	}
	person = string(boardy.RolePerson)
	harness, session := boardy.HarnessClaudeCode, os.Getenv(claudeCodeSessionEnv)
	inHarness := session != "" || os.Getenv(claudeCodeEnv) != ""
	if thread := os.Getenv(codexThreadEnv); thread != "" {
		harness, session, inHarness = boardy.HarnessCodex, thread, true
	}
	switch {
	case !inHarness && as == boardy.OrchestratorName:
		return as, person, as
	case !inHarness:
		return cmp.Or(as, person), person, ""
	case as == "" || as == boardy.OrchestratorName:
		actor = boardy.HarnessActor(harness, session, "")
		return actor, person, actor
	}
	return boardy.HarnessActor(harness, session, as), person, ""
}

func (c boardyCall) manage(subcommand string) int {
	switch subcommand {
	case "manager":
		return c.managers()
	case "triage":
		return c.triage()
	case "hand":
		return c.hand()
	case "assign":
		if len(c.args) != 2 {
			return c.o.usage(errors.New("name a " + c.words.Ticket + " and who it goes to"))
		}
		ticket, err := c.managed.Assign(c.args[0], c.args[1], c.actor)
		return c.receipt(ticket, err, cli.Changed, ticket.ID+" is assigned to "+ticket.Assignee)
	case "widen":
		if len(c.args) != 2 {
			return c.o.usage(errors.New("name a " + c.words.Ticket + " and the globs to add"))
		}
		ticket, err := c.managed.Widen(c.args[0], splitList(c.args[1]), c.actor)
		return c.receipt(ticket, err, cli.Changed, ticket.ID+" owns "+strings.Join(ticket.Owns, ", "))
	}
	return c.check()
}

func (c boardyCall) managers() int {
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
		board, err = c.managed.AddManager(key, c.args[1], c.actor)
	case c.args[0] == "remove":
		board, err = c.managed.RemoveManager(key, c.args[1], c.actor)
	default:
		return c.o.usage(fmt.Errorf("%q is not add, remove or list", c.args[0]))
	}
	if err != nil {
		return c.o.fail(err)
	}
	return c.o.done(true, board, func(page cli.Page) []string {
		facts := []string{plural(len(board.Managers), "manager")}
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

func (c boardyCall) triage() int {
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
			lines := page.Title(key+" triage", []string{plural(len(requests), "request")}, cli.Verdict{})
			for _, req := range requests {
				lines = append(lines, page.Rows([]cli.Row{{Mark: cli.Idle, Cells: []string{req.ID, req.By, req.Ticket.Title}}})...)
			}
			return lines
		})
	case len(c.args) == 2 && c.args[0] == "accept":
		ticket, err := c.managed.Accept(key, c.args[1], c.actor)
		return c.receipt(ticket, err, cli.Added, c.args[1]+" is "+ticket.ID+" "+ticket.Title)
	case len(c.args) == 2 && c.args[0] == "drop":
		if err := c.managed.Drop(key, c.args[1], c.flags["reason"], c.actor); err != nil {
			return c.o.fail(err)
		}
		return c.o.done(true, map[string]string{"dropped": c.args[1]}, func(page cli.Page) []string {
			return []string{page.Receipt(cli.Removed, "dropped "+c.args[1]+" from the triage of "+key, "")}
		})
	}
	return c.o.usage(errors.New("list, accept <request> or drop <request>"))
}

func (c boardyCall) hand() int {
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
	landed, err := c.managed.Hand(c.args[0], c.args[1], to, c.actor)
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

func (c boardyCall) check() int {
	if len(c.args) != 2 {
		return c.o.usage(errors.New("name an event and a " + c.words.Ticket))
	}
	event, err := rule.ParseEvent(c.args[0])
	if err != nil {
		return c.o.usage(err)
	}
	var to boardy.Status
	if raw := c.flags["to"]; raw != "" {
		if to, err = boardy.ParseStatus(raw); err != nil {
			return c.o.usage(err)
		}
	}
	id := c.args[1]
	fired, err := c.managed.Check(event, id, to, c.actor)
	var refused boardy.RefusedError
	var waiting boardy.WaitingError
	if err != nil && !errors.As(err, &refused) && !errors.As(err, &waiting) {
		return c.o.fail(err)
	}
	answer := boardyChecked{Event: event, Ticket: id, Fired: append([]boardy.Fired{}, fired...)}
	if err != nil {
		answer.Refused = err.Error()
	}
	return c.o.done(err == nil, answer, func(page cli.Page) []string {
		blocked := 0
		rows := make([]cli.Row, 0, len(fired)+1)
		for _, f := range fired {
			mark := cli.Warn
			if f.Blocked {
				mark, blocked = cli.Fail, blocked+1
			}
			rows = append(rows, cli.Row{Mark: mark, Cells: []string{f.RuleID, string(f.Mode)}, Detail: f.Text()})
		}
		verdict := firesVerdict(len(fired), blocked)
		if err != nil {
			verdict = cli.Verdict{Mark: cli.Fail, Text: "refused"}
			rows = append(rows, cli.Row{Mark: cli.Fail, Cells: []string{answer.Refused}})
		}
		return append(page.Title("check "+string(event)+" "+id, nil, verdict), page.Rows(rows)...)
	})
}

type boardyChecked struct {
	Event   rule.Event     `json:"event"`
	Ticket  string         `json:"ticket"`
	Fired   []boardy.Fired `json:"fired"`
	Refused string         `json:"refused,omitempty"`
}
