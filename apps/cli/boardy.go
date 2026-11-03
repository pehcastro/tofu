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

	"tofu/internal/cli/output"
	"tofu/internal/cli/page"
	"tofu/internal/host"
	"tofu/internal/librarian/rules"
	"tofu/internal/project/boardy"
	settingspkg "tofu/internal/store/settings"
)

const boardyUsage = "tofu boardy init|new|list|show|move|log|set|lint|boards|held|manager|triage|assign|hand|widen|check|tick|fire|rule|bypass|request|view|audit|report|attention|epic|sprint|ask|catchup [--json]"

func boardyUsageOf(subcommand string) string {
	switch subcommand {
	case "init":
		return "tofu boardy init --key KEY [--name text] [--mode default|empty|custom] [--hooks id,id] [--json]"
	case "new":
		return "tofu boardy new [--board KEY | --parent ID] [--type story|task|bug|subtask] [--priority P0..P4] [--status s] [--points n] [--size S|M|L] [--spec path] [--owns a,b]... [--labels docs-only,...]... [--epic E] [--sprint S] [--problem text] [--scope text] --before text --driven text --still-works line... --error-case text [--feasibility text] [--acceptance line]... [--as name] <title> [--json]"
	case "list":
		return "tofu boardy list [--board KEY] [--status s] [--all] [--fields id,status,assignee,owns] [--json]"
	case "held":
		return "tofu boardy held <path> [--json]"
	case "show":
		return "tofu boardy show <ticket> [--thread] [--json]"
	case "move":
		return "tofu boardy move <ticket> <status> [--reason text] [--bypass <hook> --why text] [--as name] [--json]"
	case "log":
		return "tofu boardy log <ticket> [<text>] [--evidence [--line n] | --before <what you ran and what it printed> [--late <commit or build>]] [--as name] [--json]"
	case "lint":
		return "tofu boardy lint [--board KEY] [--json]"
	case "boards":
		return "tofu boardy boards [--json]"
	case "set":
		return "tofu boardy set <ticket | request [--board KEY]> [--title text] [--owns a,b]... [--epic E] [--sprint S] [--acceptance line]... [--reason text] [--before text] [--driven text] [--still-works line]... [--error-case text] [--feasibility text] [--as name] [--json]"
	case "tick":
		return "tofu boardy tick [--board KEY] [--json]"
	case "fire":
		return "tofu boardy fire agent.spawn|agent.report|lead.attach [ticket] [--board KEY] [--when before|after] [--as name] [--json]"
	case "rule":
		return boardyRuleUsage
	case "bypass":
		return boardyBypassUsage
	case "request":
		return "tofu boardy request new <words> [--tickets A,B]|add <R> <ticket>...|review <R> pass|fail --note text|show <R>|list [--board KEY] [--as name] [--json]"
	}
	return cmp.Or(boardyManageUsage(subcommand), boardyViewUsageOf(subcommand), boardyDecideUsage(subcommand))
}

type boardyCall struct {
	o       output.Verb
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
	o := output.Verb{Name: "boardy", UsageLine: boardyUsage, AsJSON: output.JSONAsked(args), Out: out, ErrOut: errOut}
	args = output.WithoutJSON(args)
	if len(args) == 0 || boardyUsageOf(args[0]) == "" {
		return o.Usage(errors.New("name a subcommand"))
	}
	o.Name, o.UsageLine = "boardy "+args[0], boardyUsageOf(args[0])
	flags, rest, err := boardyFlags(args[1:])
	if err != nil {
		return o.Usage(err)
	}
	dir, err := os.Getwd()
	var store boardy.Store
	if err == nil {
		store, err = boardy.OpenStore(dir)
	}
	flow, flowErr := boardy.ParseFlow(settingspkg.TextSaying(dir, boardy.FlowSetting, func(line string) { _, _ = fmt.Fprintln(errOut, line) }))
	if err = errors.Join(err, flowErr); err != nil {
		return o.Fail(err)
	}
	actor, lead := boardyIdentity(flags["as"])
	call := boardyCall{o: o, dir: dir, managed: boardy.Managed{Local: boardy.Local{Store: store}, Person: string(boardy.RolePerson), Lead: lead}, actor: actor, store: store, words: flow.Words(), flags: flags, args: rest}
	sizes := store.EventSizes()
	written := host.BoardyWriting(dir, actor)
	code := call.run(args[0])
	written()
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
	case "bypass":
		return call.bypass()
	case "request":
		return call.request()
	case "view":
		return call.view()
	case "audit":
		return call.audit()
	case "report":
		return call.report()
	case "attention":
		return call.attention()
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
		case name == "all" || name == "answers" || name == "thread" || name == "evidence":
			flags[name] = "true"
		case i+1 == len(args):
			return nil, nil, fmt.Errorf("--%s needs a value", name)
		case (name == "acceptance" || name == "still-works") && flags[name] != "":
			flags[name], i = flags[name]+"\n"+args[i+1], i+1
		case (name == "owns" || name == "labels") && flags[name] != "":
			flags[name], i = flags[name]+","+args[i+1], i+1
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
		return c.o.Usage(fmt.Errorf("unexpected %q", c.args[0]))
	}
	board, err := c.store.InitBoard(c.flags["key"], c.flags["name"], boardy.InitMode(c.flags["mode"]), splitList(c.flags["hooks"]))
	if err != nil {
		return c.o.Fail(err)
	}
	return c.o.Done(true, board, func(page cli.Page) []string {
		return []string{page.Receipt(cli.Added, "made the "+c.words.Board+" "+board.Key, board.Dir)}
	})
}

func (c boardyCall) create() int {
	title := strings.TrimSpace(strings.Join(c.args, " "))
	if title == "" {
		return c.o.Usage(errors.New("a " + c.words.Ticket + " needs a title"))
	}
	key, err := c.board()
	if parent := c.flags["parent"]; parent != "" {
		var ref boardy.Ref
		ref, err = boardy.SplitID(parent)
		key = ref.Key
	}
	if err != nil {
		return c.o.Fail(err)
	}
	ticket := boardy.Ticket{Front: boardy.Front{Title: title, Owns: splitList(c.flags["owns"]), Labels: splitList(c.flags["labels"]), Spec: c.flags["spec"], Parent: c.flags["parent"], Epic: c.flags["epic"], Sprint: c.flags["sprint"]}, Problem: c.flags["problem"], Scope: c.flags["scope"]}
	if acceptance := c.flags["acceptance"]; acceptance != "" {
		ticket.Acceptance = []string{acceptance}
	}
	for _, drafted := range ticket.Drafted() {
		*drafted.Body = c.flags[strings.ReplaceAll(drafted.Field, "_", "-")]
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
		return c.o.Usage(err)
	}
	ticket, err = c.managed.Create(key, ticket, c.actor)
	if ticket.ID == "" {
		return c.receipt(ticket, err, cli.Added, ticket.Reason+": "+ticket.Title)
	}
	return c.receipt(ticket, err, cli.Added, ticket.ID+" "+ticket.Title)
}

func (c boardyCall) receipt(ticket boardy.Ticket, err error, mark cli.Mark, text string) int {
	if err != nil {
		return c.o.Fail(err)
	}
	path := ""
	if ticket.ID != "" {
		path, _ = c.store.TicketPath(ticket.ID)
	}
	return c.o.Done(true, ticket, func(page cli.Page) []string { return []string{page.Receipt(mark, text, path)} })
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
		return c.o.Fail(err)
	}
	tickets = slices.DeleteFunc(tickets, func(t boardy.Ticket) bool { return c.hidden(t.Status) })
	report := boardyList{Board: key, Words: c.words, Tickets: tickets}
	return c.o.Done(true, report, func(page cli.Page) []string {
		lines := page.Title(key, []string{c.words.Board, output.Plural(len(tickets), c.words.Ticket)}, cli.Verdict{})
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
			return c.o.Usage(fmt.Errorf("%q is not a front matter field: %s", field, strings.Join(known, ", ")))
		}
	}
	key, err := c.board()
	var fronts []boardy.Front
	if err == nil {
		fronts, err = c.store.Fronts(key)
	}
	if err != nil {
		return c.o.Fail(err)
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
	return c.o.Done(true, map[string]any{"board": key, "tickets": picked}, func(page cli.Page) []string {
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
		return append(page.Title(key, []string{c.words.Board, output.Plural(len(picked), c.words.Ticket)}, cli.Verdict{}), page.Rows(rows)...)
	})
}

func (c boardyCall) held() int {
	if len(c.args) != 1 {
		return c.o.Usage(errors.New("name one path"))
	}
	path := c.args[0]
	if rel, err := filepath.Rel(c.dir, path); filepath.IsAbs(path) && err == nil && filepath.IsLocal(rel) {
		path = rel
	}
	hold, err := c.store.Held(filepath.ToSlash(path))
	if err != nil {
		return c.o.Fail(err)
	}
	return c.o.Done(true, hold, func(page cli.Page) []string {
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
		return c.o.Usage(errors.New("name one " + c.words.Ticket))
	}
	ticket, err := c.managed.Get(c.args[0])
	shown := boardyShown{Ticket: ticket}
	if err == nil && c.flags["thread"] != "" {
		shown.Thread, err = c.store.Thread(ticket.ID)
	}
	if err != nil {
		return c.o.Fail(err)
	}
	path, _ := c.store.TicketPath(ticket.ID)
	return c.o.Done(true, shown, func(page cli.Page) []string {
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
		lines = append(lines, ticketSections(page, ticket)...)
		if c.flags["thread"] != "" {
			var activity []string
			evidence := 0
			for _, event := range shown.Thread {
				if event.Kind == boardy.Evidence {
					evidence++
					continue
				}
				activity = append(activity, threadLine(event))
			}
			if evidence > 0 {
				activity = append(activity, fmt.Sprintf("%d evidence entries, kept in the Log for review", evidence))
			}
			lines = append(lines, boardySection(page, "Activity", strings.Join(activity, "\n"))...)
		}
		return lines
	})
}

func boardySection(page cli.Page, name, body string) []string {
	if body == "" {
		body = page.Label("empty")
	}
	return append([]string{"", page.Subject(name)}, cli.Indent(strings.Split(body, "\n")...)...)
}

func ticketSections(page cli.Page, ticket boardy.Ticket) []string {
	lines := append(boardySection(page, "Problem", ticket.Problem), boardySection(page, "Scope", ticket.Scope)...)
	for _, drafted := range ticket.Drafted() {
		if *drafted.Body != "" {
			lines = append(lines, boardySection(page, drafted.Heading, *drafted.Body)...)
		}
	}
	for i, revision := range ticket.Acceptance {
		lines = append(lines, boardySection(page, boardy.RevisionHeading(i), revision)...)
	}
	if len(ticket.Questions) > 0 {
		lines = append(lines, boardySection(page, boardy.QuestionsHeading, boardy.QuestionsMarkdown(ticket.Questions))...)
	}
	return append(lines, boardySection(page, "Log", ticket.Log)...)
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
	kind := string(event.Kind)
	if event.Kind == boardy.Commented {
		kind = "said"
	}
	line := event.At.Local().Format(time.DateTime) + " " + kind + " " + dash(who)
	if event.From != "" || event.To != "" {
		line += " " + dash(string(event.From)) + " to " + string(event.To)
	}
	if event.Text != "" {
		line += ": " + event.Text
	}
	if len(event.Files) > 0 {
		line += " (wrote " + strings.Join(event.Files, ", ") + ")"
	}
	if event.Why != "" {
		line += "\n  why " + event.Why
	}
	if event.Default != "" {
		line += "\n  default " + event.Default
	}
	if event.Unanswered != "" {
		line += "\n  unanswered, went with the default: " + event.Unanswered
	}
	if event.Verdict != "" {
		line += "\n  verdict " + string(event.Verdict)
	}
	for i, accepted := range event.Acceptance {
		line += fmt.Sprintf("\n  line %d %s: %s", i+1, accepted.State, accepted.Line)
	}
	return line
}

func (c boardyCall) move() int {
	if len(c.args) != 2 {
		return c.o.Usage(errors.New("name a " + c.words.Ticket + " and a status"))
	}
	to, err := boardy.ParseStatus(c.args[1])
	if err != nil {
		return c.o.Usage(err)
	}
	if c.flags["why"] != "" && c.flags["bypass"] == "" {
		return c.o.Usage(errors.New("--why says why a --bypass <hook> is asked"))
	}
	c.managed.Bypass = boardy.Bypassing{Hook: c.flags["bypass"], Why: c.flags["why"]}
	before, _ := c.store.Get(c.args[0])
	ticket, err := c.managed.Move(c.args[0], to, c.flags["reason"], c.actor)
	said := ticket.ID + " is " + string(ticket.Status) + ", was " + string(before.Status)
	if before.Status == ticket.Status {
		said = ticket.ID + " is already " + string(ticket.Status)
	}
	return c.receipt(ticket, err, cli.Changed, said)
}

func (c boardyCall) log() int {
	before, capturing := c.flags["before"]
	ranOn, late := c.flags["late"]
	if strings.HasPrefix(ranOn, "--") {
		ranOn = ""
	}
	if len(c.args) < 1 || len(c.args) < 2 && !capturing {
		return c.o.Usage(errors.New("name a " + c.words.Ticket + " and the text to log"))
	}
	text := strings.Join(c.args[1:], " ")
	_, firstNotTicket := boardy.SplitID(c.args[0])
	if _, beforeNotTicket := boardy.SplitID(before); capturing && firstNotTicket != nil && beforeNotTicket == nil {
		return c.o.Usage(fmt.Errorf("--before takes the text and the %s comes first: tofu boardy log %s --before %q", c.words.Ticket, before, strings.Join(c.args, " ")))
	}
	var ticket boardy.Ticket
	var err error
	switch line := c.flags["line"]; {
	case c.flags["evidence"] == "" && line != "":
		return c.o.Usage(errors.New("--line names the acceptance line an --evidence entry proves"))
	case capturing && c.flags["evidence"] != "":
		return c.o.Usage(errors.New("an entry is a --before capture or --evidence, not both"))
	case late && !capturing:
		return c.o.Usage(errors.New("--late names the pre-change build or commit a --before ran on: tofu boardy log " + c.args[0] + " --before <what it printed> --late <commit or build>"))
	case late:
		ticket, err = c.managed.CaptureLateBefore(c.args[0], strings.TrimSpace(before+" "+text), ranOn, c.actor)
	case capturing:
		ticket, err = c.managed.CaptureBefore(c.args[0], strings.TrimSpace(before+" "+text), c.actor)
	case c.flags["evidence"] == "":
		ticket, err = c.managed.Comment(c.args[0], text, c.actor)
	default:
		number := 0
		if line != "" {
			if number, err = strconv.Atoi(line); err != nil {
				return c.o.Usage(fmt.Errorf("--line is a number counted from 1: %w", err))
			}
		}
		ticket, err = c.managed.Evidence(c.args[0], text, c.actor, number)
	}
	return c.receipt(ticket, err, cli.Changed, "logged on "+ticket.ID)
}

func (c boardyCall) plan() int {
	if len(c.args) != 1 {
		return c.o.Usage(errors.New("name one " + c.words.Ticket))
	}
	id := c.args[0]
	if _, notTicket := boardy.SplitID(id); notTicket != nil {
		return c.fillRequest(id, notTicket)
	}
	title, retitle := c.flags["title"]
	owns, reown := c.flags["owns"]
	acceptance, reaccept := c.flags["acceptance"]
	var ticket boardy.Ticket
	var err error
	if retitle {
		ticket, err = c.managed.Retitle(id, title, c.actor)
	}
	if err == nil && reown {
		ticket, err = c.managed.Reown(id, splitList(owns), c.actor)
	}
	if err == nil && reaccept {
		ticket, err = c.managed.Reaccept(id, acceptance, c.flags["reason"], c.actor)
	}
	redrafted := c.redraft(func(field, body string) error {
		if err == nil {
			ticket, err = c.managed.Redraft(id, field, body, c.actor)
		}
		return err
	})
	if err == nil && (c.flags["epic"] != "" || c.flags["sprint"] != "" || !retitle && !reown && !reaccept && redrafted == nil) {
		ticket, err = c.managed.Plan(id, c.flags["epic"], c.flags["sprint"], c.actor)
	}
	summary := ticket.ID + " " + ticket.Title + ": owns " + dash(strings.Join(ticket.Owns, ", ")) + ", " + c.words.Epic + " " + dash(ticket.Epic) + ", " + c.words.Sprint + " " + dash(ticket.Sprint)
	if redrafted != nil {
		summary += ", set " + strings.Join(redrafted, ", ")
	}
	return c.receipt(ticket, err, cli.Changed, summary)
}

func (c boardyCall) fillRequest(id string, notTicket error) int {
	key, err := c.board()
	var request boardy.Ticket
	redrafted := c.redraft(func(field, body string) error {
		if err == nil {
			request, err = c.managed.RedraftRequest(key, id, field, body, c.actor)
		}
		return err
	})
	if err == nil && redrafted == nil {
		err = notTicket
	}
	return c.receipt(request, err, cli.Changed, "request "+id+" "+request.Title+": set "+strings.Join(redrafted, ", "))
}

func (c boardyCall) redraft(set func(field, body string) error) []string {
	var redrafted []string
	for _, drafted := range new(boardy.Ticket).Drafted() {
		if body, given := c.flags[strings.ReplaceAll(drafted.Field, "_", "-")]; given && set(drafted.Field, body) == nil {
			redrafted = append(redrafted, drafted.Heading)
		}
	}
	return redrafted
}

func (c boardyCall) tick() int {
	boards, err := c.named()
	ran := map[string][]string{}
	for at := 0; err == nil && at < len(boards); at++ {
		ran[boards[at].Key], err = c.managed.Tick(boards[at].Key)
	}
	if err != nil {
		return c.o.Fail(err)
	}
	return c.o.Done(true, ran, func(page cli.Page) []string {
		rows := make([]cli.Row, 0, len(boards))
		for _, board := range boards {
			text := "nothing due"
			if hooks := ran[board.Key]; len(hooks) > 0 {
				text = "ran " + strings.Join(hooks, ", ")
			}
			rows = append(rows, cli.Row{Mark: cli.Done, Cells: []string{board.Key, text}})
		}
		return append(page.Title("tick", []string{output.Plural(len(boards), c.words.Board)}, cli.Verdict{}), page.Rows(rows)...)
	})
}

func (c boardyCall) fire() int {
	if len(c.args) < 1 || len(c.args) > 2 {
		return c.o.Usage(errors.New("name an event, and a " + c.words.Ticket + " unless it is " + string(rule.HookLeadAttach)))
	}
	event, when := rule.HookEvent(c.args[0]), rule.When(cmp.Or(c.flags["when"], string(rule.WhenAfter)))
	if !slices.Contains([]rule.HookEvent{rule.HookAgentSpawn, rule.HookAgentReport, rule.HookLeadAttach}, event) {
		return c.o.Usage(fmt.Errorf("%q is not fired by hand: %s, %s or %s, and every other event fires from the command that does it", event, rule.HookAgentSpawn, rule.HookAgentReport, rule.HookLeadAttach))
	}
	if when != rule.WhenBefore && when != rule.WhenAfter {
		return c.o.Usage(fmt.Errorf("--when is %s or %s", rule.WhenBefore, rule.WhenAfter))
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
		return c.o.Fail(err)
	}
	return c.o.Done(true, map[string]string{"event": string(event), "when": string(when), "board": key, "ticket": id}, func(page cli.Page) []string {
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
		return c.o.Fail(err)
	}
	findings := c.store.Lint(boards)
	return c.o.Done(len(findings) == 0, findings, func(page cli.Page) []string {
		verdict := cli.Verdict{Mark: cli.Done, Text: "clean"}
		if len(findings) > 0 {
			verdict = cli.Verdict{Mark: cli.Fail, Text: strconv.Itoa(len(findings)) + " found"}
		}
		rows := make([]cli.Row, 0, len(findings))
		for _, finding := range findings {
			rows = append(rows, cli.Row{Mark: cli.Fail, Cells: []string{dash(finding.Ticket), finding.What}})
		}
		return append(page.Title("lint", []string{output.Plural(len(boards), c.words.Board)}, verdict), page.Rows(rows)...)
	})
}

func (c boardyCall) boards() int {
	if len(c.args) > 0 {
		return c.o.Usage(fmt.Errorf("unexpected %q", c.args[0]))
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
		return c.o.Fail(err)
	}
	return c.o.Done(true, report, func(page cli.Page) []string {
		lines := page.Title(c.words.Board+"s", []string{strconv.Itoa(len(report)) + " in this project"}, cli.Verdict{})
		if len(report) == 0 {
			return append(lines, page.Label("none yet: tofu boardy init --key KEY"))
		}
		rows := make([]cli.Row, 0, len(report))
		for _, board := range report {
			rows = append(rows, cli.Row{Mark: cli.Idle, Cells: []string{board.Key, output.Plural(board.Tickets, c.words.Ticket) + ", " + strconv.Itoa(board.Live) + " live"}, Detail: page.Path(board.Dir)})
		}
		return append(lines, page.Rows(rows)...)
	})
}

func requestMark(state boardy.RequestState) cli.Mark {
	switch state {
	case boardy.RequestDone:
		return cli.Done
	case boardy.RequestUnreviewed:
		return cli.Warn
	case boardy.RequestRejected:
		return cli.Fail
	}
	return cli.Active
}

func (c boardyCall) request() int {
	if len(c.args) == 0 {
		return c.o.Usage(errors.New("name new, add, review, show or list"))
	}
	key, err := c.board()
	if err != nil {
		return c.o.Fail(err)
	}
	verb, rest := c.args[0], c.args[1:]
	if verb == "list" {
		standings, err := c.store.RequestStandings(key)
		if err != nil {
			return c.o.Fail(err)
		}
		return c.o.Done(true, standings, func(page cli.Page) []string {
			rows := make([]cli.Row, 0, len(standings))
			for _, s := range standings {
				said, _, _ := strings.Cut(strings.TrimSpace(s.Request.Words), "\n")
				rows = append(rows, cli.Row{Mark: requestMark(s.State), Cells: []string{s.Request.ID, string(s.State), output.Plural(len(s.Request.Tickets), c.words.Ticket), said}})
			}
			return append(page.Title(key+" requests", []string{strconv.Itoa(len(standings))}, cli.Verdict{}), page.Rows(rows)...)
		})
	}
	var standing boardy.RequestStanding
	switch {
	case verb == "new" && len(rest) > 0:
		standing, err = c.managed.NewRequest(key, strings.Join(rest, " "), splitList(c.flags["tickets"]), c.actor)
	case verb == "add" && len(rest) > 1:
		standing, err = c.managed.AddToRequest(key, rest[0], rest[1:], c.actor)
	case verb == "review" && len(rest) == 2:
		var verdict boardy.ReviewVerdict
		if verdict, err = boardy.ParseReviewVerdict(rest[1]); err != nil {
			return c.o.Usage(err)
		}
		standing, err = c.managed.ReviewRequest(key, rest[0], verdict, c.flags["note"], c.actor)
	case verb == "show" && len(rest) == 1:
		standing, err = c.store.RequestStanding(key, rest[0])
	default:
		return c.o.Usage(fmt.Errorf("request %s takes other arguments", verb))
	}
	if err != nil {
		return c.o.Fail(err)
	}
	r := standing.Request
	return c.o.Done(true, standing, func(page cli.Page) []string {
		lines := page.Title(key+" request "+r.ID, []string{output.Plural(len(r.Tickets), c.words.Ticket)}, cli.Verdict{Mark: requestMark(standing.State), Text: string(standing.State)})
		facts := []cli.Fact{{Label: "why", Text: standing.Why}, {Label: "by", Text: r.By + " " + r.At.Local().Format(time.DateTime)}}
		if review := r.Review; review != nil {
			facts = append(facts, cli.Fact{Label: "review", Text: string(review.Verdict) + " by " + review.By + " " + review.At.Local().Format(time.DateTime) + ": " + review.Note})
		}
		lines = append(append(lines, page.Facts(facts)...), boardySection(page, "Words", r.Words)...)
		rows := make([]cli.Row, 0, len(standing.Tickets))
		for _, ticket := range standing.Tickets {
			rows = append(rows, cli.Row{Mark: statusMark(ticket.Status), Cells: []string{ticket.ID, string(ticket.Status), ticket.Title}})
		}
		return append(append(lines, "", page.Subject(c.words.Ticket+"s")), page.Rows(rows)...)
	})
}
