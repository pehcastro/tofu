package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/interface/cli"
	"tofu/internal/boardy"
	"tofu/internal/rule"
)

const boardyUsage = "tofu boardy init|new|list|show|move|log|set|lint|boards|held|manager|triage|assign|hand|widen|check|tick|fire|rule|view|audit|report|epic|sprint|ask|catchup [--json]"

func boardyUsageOf(subcommand string) string {
	switch subcommand {
	case "init":
		return "tofu boardy init --key KEY [--name text] [--json]"
	case "new":
		return "tofu boardy new [--board KEY | --parent ID] [--type story|task|bug|subtask] [--priority P0..P4] [--status s] [--points n] [--size S|M|L] [--spec path] [--owns a,b] [--epic E] [--sprint S] [--scope text] [--acceptance text] [--as name] <title> [--json]"
	case "list":
		return "tofu boardy list [--board KEY] [--status s] [--all] [--fields id,status,assignee,owns] [--json]"
	case "held":
		return "tofu boardy held <path> [--json]"
	case "show":
		return "tofu boardy show <ticket> [--thread] [--json]"
	case "move":
		return "tofu boardy move <ticket> <status> [--reason text] [--as name] [--json]"
	case "log":
		return "tofu boardy log <ticket> <text> [--as name] [--json]"
	case "lint":
		return "tofu boardy lint [--board KEY] [--json]"
	case "boards":
		return "tofu boardy boards [--json]"
	case "set":
		return "tofu boardy set <ticket> [--epic E] [--sprint S] [--as name] [--json]"
	case "tick":
		return "tofu boardy tick [--board KEY] [--json]"
	case "fire":
		return "tofu boardy fire agent.spawn|agent.report|lead.attach [ticket] [--board KEY] [--when before|after] [--as name] [--json]"
	case "rule":
		return boardyRuleUsage
	}
	return cmp.Or(boardyManageUsage(subcommand), boardyViewUsageOf(subcommand), boardyDecideUsage(subcommand))
}

type boardyCall struct {
	o       verbOutput
	dir     string
	managed boardy.Managed
	actor   string
	store   boardy.Store
	words   boardy.Words
	flags   map[string]string
	args    []string
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
	actor, person, lead := boardyIdentity(flags["as"])
	call := boardyCall{o: o, dir: dir, managed: boardy.Managed{Local: boardy.Local{Store: store}, Person: person, Lead: lead}, actor: actor, store: store, words: flow.Words(), flags: flags, args: rest}
	sizes := store.EventSizes()
	code := call.run(args[0])
	for _, nudge := range store.NudgesSince(sizes) {
		_, _ = fmt.Fprintln(errOut, "nudge "+nudge)
	}
	return code
}

func (call boardyCall) run(subcommand string) int {
	switch subcommand {
	case "manager", "triage", "hand", "assign", "widen", "check":
		return call.manage(subcommand)
	case "set":
		return call.plan()
	case "tick":
		return call.tick()
	case "fire":
		return call.fire()
	case "rule":
		return call.rule()
	case "view":
		return call.view()
	case "audit":
		return call.audit()
	case "report":
		return call.report()
	case "epic":
		return call.epic()
	case "sprint":
		return call.sprint()
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
	case "held":
		return call.held()
	case "ask":
		return call.ask()
	case "catchup":
		return call.catchup()
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
		case name == "all" || name == "answers" || name == "thread":
			flags[name] = "true"
		case i+1 == len(args):
			return nil, nil, fmt.Errorf("--%s needs a value", name)
		default:
			flags[name], i = args[i+1], i+1
		}
	}
	return flags, rest, nil
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
	if err := c.store.WriteStarterHooks(board.Key); err != nil {
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
	if parent := c.flags["parent"]; parent != "" {
		var ref boardy.Ref
		ref, err = boardy.SplitID(parent)
		key = ref.Key
	}
	if err != nil {
		return c.o.fail(err)
	}
	ticket := boardy.Ticket{Front: boardy.Front{Title: title, Owns: splitList(c.flags["owns"]), Spec: c.flags["spec"], Parent: c.flags["parent"], Epic: c.flags["epic"], Sprint: c.flags["sprint"]}, Scope: c.flags["scope"]}
	if acceptance := c.flags["acceptance"]; acceptance != "" {
		ticket.Acceptance = []string{acceptance}
	}
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
	ticket.Size, err = boardy.ParseSize(c.flags["size"])
	parseErrs = append(parseErrs, err)
	if err := errors.Join(parseErrs...); err != nil {
		return c.o.usage(err)
	}
	ticket, err = c.managed.Create(key, ticket, c.actor)
	if ticket.ID == "" {
		return c.receipt(ticket, err, cli.Added, ticket.Reason+": "+ticket.Title)
	}
	return c.receipt(ticket, err, cli.Added, ticket.ID+" "+ticket.Title)
}

func (c boardyCall) receipt(ticket boardy.Ticket, err error, mark cli.Mark, text string) int {
	if err != nil {
		return c.o.fail(err)
	}
	path := ""
	if ticket.ID != "" {
		path, _ = c.store.TicketPath(ticket.ID)
	}
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

func (c boardyCall) hidden(status boardy.Status) bool {
	wanted := c.flags["status"]
	return wanted != "" && string(status) != wanted || wanted == "" && c.flags["all"] == "" && !status.Live()
}

func (c boardyCall) list() int {
	if fields := c.flags["fields"]; fields != "" {
		return c.listFields(splitList(fields))
	}
	key, err := c.board()
	var tickets []boardy.Ticket
	if err == nil {
		tickets, err = c.managed.List(key)
	}
	if err != nil {
		return c.o.fail(err)
	}
	tickets = slices.DeleteFunc(tickets, func(t boardy.Ticket) bool { return c.hidden(t.Status) })
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

func (c boardyCall) listFields(fields []string) int {
	shape := reflect.TypeFor[boardy.Front]()
	known := make([]string, shape.NumField())
	for i := range known {
		known[i], _, _ = strings.Cut(shape.Field(i).Tag.Get("json"), ",")
	}
	for _, field := range fields {
		if !slices.Contains(known, field) {
			return c.o.usage(fmt.Errorf("%q is not a front matter field: %s", field, strings.Join(known, ", ")))
		}
	}
	key, err := c.board()
	var fronts []boardy.Front
	if err == nil {
		fronts, err = c.store.Fronts(key)
	}
	if err != nil {
		return c.o.fail(err)
	}
	picked := []map[string]any{}
	var marks []cli.Mark
	for _, front := range fronts {
		if c.hidden(front.Status) {
			continue
		}
		marks = append(marks, statusMark(front.Status))
		raw, _ := json.Marshal(front)
		var all map[string]any
		_ = json.Unmarshal(raw, &all)
		one := map[string]any{}
		for _, field := range fields {
			one[field] = all[field]
		}
		picked = append(picked, one)
	}
	return c.o.done(true, map[string]any{"board": key, "tickets": picked}, func(page cli.Page) []string {
		rows := []cli.Row{{Cells: fields}}
		for at, one := range picked {
			cells := make([]string, len(fields))
			for i, field := range fields {
				text, isText := one[field].(string)
				if !isText && one[field] != nil {
					raw, _ := json.Marshal(one[field])
					text = string(raw)
				}
				cells[i] = dash(text)
			}
			rows = append(rows, cli.Row{Mark: marks[at], Cells: cells})
		}
		return append(page.Title(key, []string{c.words.Board, plural(len(picked), c.words.Ticket)}, cli.Verdict{}), page.Rows(rows)...)
	})
}

func (c boardyCall) held() int {
	if len(c.args) != 1 {
		return c.o.usage(errors.New("name one path"))
	}
	path := c.args[0]
	if rel, err := filepath.Rel(c.dir, path); filepath.IsAbs(path) && err == nil && filepath.IsLocal(rel) {
		path = rel
	}
	hold, err := c.store.Held(filepath.ToSlash(path))
	if err != nil {
		return c.o.fail(err)
	}
	return c.o.done(true, hold, func(page cli.Page) []string {
		if !hold.Held {
			return []string{page.Receipt(cli.Idle, hold.Path+" is not held", "")}
		}
		return []string{page.Receipt(cli.Active, hold.Path+" is held by "+hold.Ticket+" ("+string(hold.Status)+") through "+hold.Glob, "")}
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
	ticket, err := c.managed.Get(c.args[0])
	shown := boardyShown{Ticket: ticket}
	if err == nil && c.flags["thread"] != "" {
		shown.Thread, err = c.store.Thread(ticket.ID)
	}
	if err != nil {
		return c.o.fail(err)
	}
	path, _ := c.store.TicketPath(ticket.ID)
	return c.o.done(true, shown, func(page cli.Page) []string {
		lines := page.Title(ticket.ID+" "+ticket.Title, []string{string(ticket.Type), string(ticket.Priority)}, cli.Verdict{Mark: statusMark(ticket.Status), Text: string(ticket.Status)})
		lines = append(lines, page.Facts([]cli.Fact{
			{Label: c.words.Board, Text: ticket.Board()},
			{Label: "reason", Text: ticket.Reason},
			{Label: "assignee", Text: dash(ticket.Assignee)},
			{Label: c.words.Epic, Text: dash(ticket.Epic)},
			{Label: c.words.Sprint, Text: dash(ticket.Sprint)},
			{Label: c.words.Points, Text: strconv.Itoa(ticket.Points)},
			{Label: "size", Text: dash(string(ticket.Size))},
			{Label: "spec", Text: dash(ticket.Spec)},
			{Label: "owns", Text: strings.Join(ticket.Owns, ", ")},
			{Label: "depends", Text: strings.Join(ticket.Depends, ", ")},
			{Label: "relates", Text: strings.Join(ticket.Relates, ", ")},
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
		if len(ticket.Questions) > 0 {
			section(boardy.QuestionsHeading, boardy.QuestionsMarkdown(ticket.Questions))
		}
		section("Log", ticket.Log)
		if c.flags["thread"] != "" {
			activity := make([]string, len(shown.Thread))
			for i, event := range shown.Thread {
				activity[i] = threadLine(event)
			}
			section("Activity", strings.Join(activity, "\n"))
		}
		return lines
	})
}

type boardyShown struct {
	boardy.Ticket
	Thread []boardy.Event `json:"thread,omitempty"`
}

func threadLine(event boardy.Event) string {
	who := cmp.Or(event.Agent, event.Actor)
	if event.Author != nil {
		who = event.Author.Name
		if event.Agent != "" && event.Agent != who {
			who += " to " + event.Agent
		}
	}
	line := event.At.Local().Format(time.DateTime) + " " + string(event.Kind) + " " + dash(who)
	if event.From != "" || event.To != "" {
		line += " " + dash(string(event.From)) + " to " + string(event.To)
	}
	if event.Text != "" {
		line += ": " + event.Text
	}
	if len(event.Files) > 0 {
		line += " (wrote " + strings.Join(event.Files, ", ") + ")"
	}
	return line
}

func (c boardyCall) move() int {
	if len(c.args) != 2 {
		return c.o.usage(errors.New("name a " + c.words.Ticket + " and a status"))
	}
	to, err := boardy.ParseStatus(c.args[1])
	if err != nil {
		return c.o.usage(err)
	}
	before, _ := c.store.Get(c.args[0])
	ticket, err := c.managed.Move(c.args[0], to, c.flags["reason"], c.actor)
	is := " is "
	if before.Status == to {
		is = " is already "
	}
	return c.receipt(ticket, err, cli.Changed, ticket.ID+is+string(ticket.Status))
}

func (c boardyCall) log() int {
	if len(c.args) < 2 {
		return c.o.usage(errors.New("name a " + c.words.Ticket + " and the text to log"))
	}
	ticket, err := c.managed.Comment(c.args[0], strings.Join(c.args[1:], " "), c.actor)
	return c.receipt(ticket, err, cli.Changed, "logged on "+ticket.ID)
}

func (c boardyCall) plan() int {
	if len(c.args) != 1 {
		return c.o.usage(errors.New("name one " + c.words.Ticket))
	}
	ticket, err := c.managed.Plan(c.args[0], c.flags["epic"], c.flags["sprint"], c.actor)
	return c.receipt(ticket, err, cli.Changed, ticket.ID+" is in "+c.words.Epic+" "+dash(ticket.Epic)+", "+c.words.Sprint+" "+dash(ticket.Sprint))
}

func (c boardyCall) tick() int {
	boards, err := c.named()
	ran := map[string][]string{}
	for at := 0; err == nil && at < len(boards); at++ {
		ran[boards[at].Key], err = c.managed.Tick(boards[at].Key)
	}
	if err != nil {
		return c.o.fail(err)
	}
	return c.o.done(true, ran, func(page cli.Page) []string {
		rows := make([]cli.Row, 0, len(boards))
		for _, board := range boards {
			text := "nothing due"
			if hooks := ran[board.Key]; len(hooks) > 0 {
				text = "ran " + strings.Join(hooks, ", ")
			}
			rows = append(rows, cli.Row{Mark: cli.Done, Cells: []string{board.Key, text}})
		}
		return append(page.Title("tick", []string{plural(len(boards), c.words.Board)}, cli.Verdict{}), page.Rows(rows)...)
	})
}

func (c boardyCall) fire() int {
	if len(c.args) < 1 || len(c.args) > 2 {
		return c.o.usage(errors.New("name an event, and a " + c.words.Ticket + " unless it is " + string(rule.HookLeadAttach)))
	}
	event, when := rule.HookEvent(c.args[0]), rule.When(cmp.Or(c.flags["when"], string(rule.WhenAfter)))
	if !slices.Contains([]rule.HookEvent{rule.HookAgentSpawn, rule.HookAgentReport, rule.HookLeadAttach}, event) {
		return c.o.usage(fmt.Errorf("%q is not fired by hand: %s, %s or %s, and every other event fires from the command that does it", event, rule.HookAgentSpawn, rule.HookAgentReport, rule.HookLeadAttach))
	}
	if when != rule.WhenBefore && when != rule.WhenAfter {
		return c.o.usage(fmt.Errorf("--when is %s or %s", rule.WhenBefore, rule.WhenAfter))
	}
	id := ""
	key, err := c.board()
	if len(c.args) == 2 {
		var ref boardy.Ref
		id = c.args[1]
		ref, err = boardy.SplitID(id)
		key = ref.Key
	}
	if err == nil {
		err = c.managed.Fire(when, event, key, id, c.actor)
	}
	if err != nil {
		return c.o.fail(err)
	}
	return c.o.done(true, map[string]string{"event": string(event), "when": string(when), "board": key, "ticket": id}, func(page cli.Page) []string {
		return []string{page.Receipt(cli.Done, "fired "+string(when)+" "+string(event)+" on "+cmp.Or(id, key), "")}
	})
}

func (c boardyCall) named() ([]boardy.Board, error) {
	key := c.flags["board"]
	if key == "" {
		return c.store.Boards()
	}
	board, err := c.store.Board(key)
	return []boardy.Board{board}, err
}

func (c boardyCall) lint() int {
	boards, err := c.named()
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
