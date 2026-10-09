package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/interface/cli"
	"tofu/internal/boardy"
)

const boardyUsage = "tofu boardy init|new|list|show|move|log|lint|boards [--json]"

func boardyUsageOf(subcommand string) string {
	switch subcommand {
	case "init":
		return "tofu boardy init --key KEY [--name text] [--json]"
	case "new":
		return "tofu boardy new [--board KEY] [--type story|task|bug|subtask] [--priority P0..P4] [--status s] [--points n] [--owns a,b] [--as name] <title> [--json]"
	case "list":
		return "tofu boardy list [--board KEY] [--status s] [--all] [--json]"
	case "show":
		return "tofu boardy show <ticket> [--json]"
	case "move":
		return "tofu boardy move <ticket> <status> [--reason text] [--as name] [--json]"
	case "log":
		return "tofu boardy log <ticket> <text> [--as name] [--json]"
	case "lint":
		return "tofu boardy lint [--board KEY] [--json]"
	case "boards":
		return "tofu boardy boards [--json]"
	}
	return ""
}

type boardyCall struct {
	o          verbOutput
	controller boardy.Controller
	store      boardy.Store
	words      boardy.Words
	flags      map[string]string
	args       []string
}

type boardyBoard struct {
	boardy.Board
	Tickets int `json:"tickets"`
	Live    int `json:"live"`
}

type boardyList struct {
	Board   string          `json:"board"`
	Words   boardy.Words    `json:"words"`
	Tickets []boardy.Ticket `json:"tickets"`
}

func boardyVerb(args []string, out, errOut io.Writer) int {
	o := verbOutput{verb: "boardy", usageLine: boardyUsage, asJSON: jsonAsked(args), out: out, errOut: errOut}
	args = withoutJSON(args)
	if len(args) == 0 || boardyUsageOf(args[0]) == "" {
		return o.usage(errors.New("name a subcommand"))
	}
	o.verb, o.usageLine = "boardy "+args[0], boardyUsageOf(args[0])
	flags, rest, err := boardyFlags(args[1:])
	if err != nil {
		return o.usage(err)
	}
	dir, err := os.Getwd()
	var store boardy.Store
	if err == nil {
		store, err = boardy.OpenStore(dir)
	}
	flow, flowErr := boardy.ParseFlow(settingText(dir, boardy.FlowSetting, func(line string) { _, _ = fmt.Fprintln(errOut, line) }))
	if err = errors.Join(err, flowErr); err != nil {
		return o.fail(err)
	}
	call := boardyCall{o: o, controller: boardy.Local{Store: store}, store: store, words: flow.Words(), flags: flags, args: rest}
	switch args[0] {
	case "init":
		return call.init()
	case "new":
		return call.create()
	case "list":
		return call.list()
	case "show":
		return call.show()
	case "move":
		return call.move()
	case "log":
		return call.log()
	case "lint":
		return call.lint()
	}
	return call.boards()
}

func boardyFlags(args []string) (map[string]string, []string, error) {
	flags := map[string]string{}
	var rest []string
	for i := 0; i < len(args); i++ {
		name, isFlag := strings.CutPrefix(args[i], "--")
		switch {
		case !isFlag:
			rest = append(rest, args[i])
		case name == "all":
			flags[name] = "true"
		case i+1 == len(args):
			return nil, nil, fmt.Errorf("--%s needs a value", name)
		default:
			flags[name], i = args[i+1], i+1
		}
	}
	return flags, rest, nil
}

func (c boardyCall) actor() string {
	if as := c.flags["as"]; as != "" {
		return as
	}
	return "person"
}

func (c boardyCall) board() (string, error) {
	if key := c.flags["board"]; key != "" {
		return key, nil
	}
	boards, err := c.store.Boards()
	switch {
	case err != nil:
		return "", err
	case len(boards) == 1:
		return boards[0].Key, nil
	case len(boards) == 0:
		return "", errors.New("this project has no " + c.words.Board + " yet: tofu boardy init --key KEY")
	}
	return "", fmt.Errorf("this project has %d %ss: name one with --board", len(boards), c.words.Board)
}

func (c boardyCall) init() int {
	if len(c.args) > 0 {
		return c.o.usage(fmt.Errorf("unexpected %q", c.args[0]))
	}
	board, err := c.store.Init(c.flags["key"], c.flags["name"])
	if err != nil {
		return c.o.fail(err)
	}
	return c.o.done(true, board, func(page cli.Page) []string {
		return []string{page.Receipt(cli.Added, "made the "+c.words.Board+" "+board.Key, board.Dir)}
	})
}

func (c boardyCall) create() int {
	title := strings.TrimSpace(strings.Join(c.args, " "))
	if title == "" {
		return c.o.usage(errors.New("a " + c.words.Ticket + " needs a title"))
	}
	key, err := c.board()
	if err != nil {
		return c.o.fail(err)
	}
	ticket := boardy.Ticket{Front: boardy.Front{Title: title, Owns: splitList(c.flags["owns"])}}
	var parseErrs []error
	if raw := c.flags["type"]; raw != "" {
		ticket.Type, err = boardy.ParseKind(raw)
		parseErrs = append(parseErrs, err)
	}
	if raw := c.flags["priority"]; raw != "" {
		ticket.Priority, err = boardy.ParsePriority(raw)
		parseErrs = append(parseErrs, err)
	}
	if raw := c.flags["status"]; raw != "" {
		ticket.Status, err = boardy.ParseStatus(raw)
		parseErrs = append(parseErrs, err)
	}
	if raw := c.flags["points"]; raw != "" {
		ticket.Points, err = strconv.Atoi(raw)
		parseErrs = append(parseErrs, err)
	}
	if err := errors.Join(parseErrs...); err != nil {
		return c.o.usage(err)
	}
	ticket, err = c.controller.Create(key, ticket, c.actor())
	return c.receipt(ticket, err, cli.Added, ticket.ID+" "+ticket.Title)
}

func (c boardyCall) receipt(ticket boardy.Ticket, err error, mark cli.Mark, text string) int {
	if err != nil {
		return c.o.fail(err)
	}
	path, _ := c.store.TicketPath(ticket.ID)
	return c.o.done(true, ticket, func(page cli.Page) []string { return []string{page.Receipt(mark, text, path)} })
}

func splitList(raw string) []string {
	var values []string
	for value := range strings.SplitSeq(raw, ",") {
		if value = strings.TrimSpace(value); value != "" {
			values = append(values, value)
		}
	}
	return values
}

func (c boardyCall) list() int {
	key, err := c.board()
	var tickets []boardy.Ticket
	if err == nil {
		tickets, err = c.controller.List(key)
	}
	if err != nil {
		return c.o.fail(err)
	}
	wanted := c.flags["status"]
	tickets = slices.DeleteFunc(tickets, func(t boardy.Ticket) bool {
		return wanted != "" && string(t.Status) != wanted || wanted == "" && c.flags["all"] == "" && !t.Status.Live()
	})
	report := boardyList{Board: key, Words: c.words, Tickets: tickets}
	return c.o.done(true, report, func(page cli.Page) []string {
		lines := page.Title(key, []string{c.words.Board, plural(len(tickets), c.words.Ticket)}, cli.Verdict{})
		if len(tickets) == 0 {
			return append(lines, page.Label("no live "+c.words.Ticket+"s: tofu boardy new <title>"))
		}
		rows := []cli.Row{{Cells: []string{c.words.Ticket, "status", "priority", c.words.Sprint, "assignee", "title"}}}
		for _, t := range tickets {
			rows = append(rows, cli.Row{Mark: statusMark(t.Status), Cells: []string{t.ID, string(t.Status), string(t.Priority), dash(t.Sprint), dash(t.Assignee), t.Title}})
		}
		return append(lines, page.Rows(rows)...)
	})
}

func statusMark(status boardy.Status) cli.Mark {
	switch status {
	case boardy.Done:
		return cli.Done
	case boardy.Doing, boardy.Review:
		return cli.Active
	case boardy.Blocked:
		return cli.Warn
	case boardy.Dropped:
		return cli.Removed
	}
	return cli.Idle
}

func dash(text string) string {
	if text == "" {
		return "-"
	}
	return text
}

func (c boardyCall) show() int {
	if len(c.args) != 1 {
		return c.o.usage(errors.New("name one " + c.words.Ticket))
	}
	ticket, err := c.controller.Get(c.args[0])
	if err != nil {
		return c.o.fail(err)
	}
	path, _ := c.store.TicketPath(ticket.ID)
	return c.o.done(true, ticket, func(page cli.Page) []string {
		lines := page.Title(ticket.ID+" "+ticket.Title, []string{string(ticket.Type), string(ticket.Priority)}, cli.Verdict{Mark: statusMark(ticket.Status), Text: string(ticket.Status)})
		lines = append(lines, page.Facts([]cli.Fact{
			{Label: c.words.Board, Text: ticket.Board()},
			{Label: "reason", Text: ticket.Reason},
			{Label: "assignee", Text: dash(ticket.Assignee)},
			{Label: c.words.Epic, Text: dash(ticket.Epic)},
			{Label: c.words.Sprint, Text: dash(ticket.Sprint)},
			{Label: c.words.Points, Text: strconv.Itoa(ticket.Points)},
			{Label: "owns", Text: strings.Join(ticket.Owns, ", ")},
			{Label: "depends", Text: strings.Join(ticket.Depends, ", ")},
			{Label: "updated", Text: ticket.Updated.Local().Format(time.DateTime)},
			{Label: "file", Text: page.Path(path)},
		})...)
		section := func(name, body string) {
			lines = append(lines, "", page.Subject(name))
			if body == "" {
				body = page.Label("empty")
			}
			lines = append(lines, cli.Indent(strings.Split(body, "\n")...)...)
		}
		section("Problem", ticket.Problem)
		section("Scope", ticket.Scope)
		for i, revision := range ticket.Acceptance {
			section(boardy.RevisionHeading(i), revision)
		}
		section("Log", ticket.Log)
		return lines
	})
}

func (c boardyCall) move() int {
	if len(c.args) != 2 {
		return c.o.usage(errors.New("name a " + c.words.Ticket + " and a status"))
	}
	to, err := boardy.ParseStatus(c.args[1])
	if err != nil {
		return c.o.usage(err)
	}
	ticket, err := c.controller.Move(c.args[0], to, c.flags["reason"], c.actor())
	return c.receipt(ticket, err, cli.Changed, ticket.ID+" is "+string(ticket.Status))
}

func (c boardyCall) log() int {
	if len(c.args) < 2 {
		return c.o.usage(errors.New("name a " + c.words.Ticket + " and the text to log"))
	}
	ticket, err := c.controller.Comment(c.args[0], strings.Join(c.args[1:], " "), c.actor())
	return c.receipt(ticket, err, cli.Changed, "logged on "+ticket.ID)
}

func (c boardyCall) lint() int {
	boards, err := c.store.Boards()
	if key := c.flags["board"]; key != "" && err == nil {
		var board boardy.Board
		board, err = c.store.Board(key)
		boards = []boardy.Board{board}
	}
	if err != nil {
		return c.o.fail(err)
	}
	findings := c.store.Lint(boards)
	return c.o.done(len(findings) == 0, findings, func(page cli.Page) []string {
		verdict := cli.Verdict{Mark: cli.Done, Text: "clean"}
		if len(findings) > 0 {
			verdict = cli.Verdict{Mark: cli.Fail, Text: strconv.Itoa(len(findings)) + " found"}
		}
		rows := make([]cli.Row, 0, len(findings))
		for _, finding := range findings {
			rows = append(rows, cli.Row{Mark: cli.Fail, Cells: []string{dash(finding.Ticket), finding.What}})
		}
		return append(page.Title("lint", []string{plural(len(boards), c.words.Board)}, verdict), page.Rows(rows)...)
	})
}

func (c boardyCall) boards() int {
	if len(c.args) > 0 {
		return c.o.usage(fmt.Errorf("unexpected %q", c.args[0]))
	}
	boards, err := c.store.Boards()
	report := make([]boardyBoard, 0, len(boards))
	for _, board := range boards {
		tickets, ticketsErr := c.store.Tickets(board.Key)
		err = errors.Join(err, ticketsErr)
		live := 0
		for _, t := range tickets {
			if t.Status.Live() {
				live++
			}
		}
		report = append(report, boardyBoard{Board: board, Tickets: len(tickets), Live: live})
	}
	if err != nil {
		return c.o.fail(err)
	}
	return c.o.done(true, report, func(page cli.Page) []string {
		lines := page.Title(c.words.Board+"s", []string{strconv.Itoa(len(report)) + " in this project"}, cli.Verdict{})
		if len(report) == 0 {
			return append(lines, page.Label("none yet: tofu boardy init --key KEY"))
		}
		rows := make([]cli.Row, 0, len(report))
		for _, board := range report {
			rows = append(rows, cli.Row{Mark: cli.Idle, Cells: []string{board.Key, plural(board.Tickets, c.words.Ticket) + ", " + strconv.Itoa(board.Live) + " live"}, Detail: page.Path(board.Dir)})
		}
		return append(lines, page.Rows(rows)...)
	})
}
