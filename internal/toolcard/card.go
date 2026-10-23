package toolcard

import (
	"cmp"
	"encoding/json"
	"path"
	"strconv"
	"strings"
)

type Family string

const (
	Explored Family = "explored"
	Edited   Family = "edited"
	Tests    Family = "tests"
	Git      Family = "git"
	Board    Family = "board"
	Memory   Family = "memory"
	Agents   Family = "agents"
	Asked    Family = "asked"
	Web      Family = "web"
	Ran      Family = "ran"
)

func Families() []Family {
	return []Family{Explored, Edited, Tests, Git, Board, Memory, Agents, Asked, Web, Ran}
}

type Tone string

const (
	Ok   Tone = "ok"
	Warn Tone = "warn"
	Fail Tone = "fail"
)

func Tones() []Tone { return []Tone{Ok, Warn, Fail} }

type Change struct {
	Path    string
	Added   int
	Removed int
}

type Card struct {
	Family     Family
	Doing      string
	Verb       string
	Object     string
	Meta       string
	Tone       Tone
	Fold       bool
	Changes    []Change
	kind       kind
	words      []string
	failedVerb string
	subject    string
	withPush   bool
	pushNote   string
	settled    bool
}

type kind int

const (
	kindRan kind = iota
	kindRead
	kindSearch
	kindList
	kindGitStatus
	kindGitDiff
	kindGitLog
	kindGitAdd
	kindCommit
	kindPush
	kindPull
	kindSwitch
	kindStash
	kindReset
	kindBoardMove
	kindBoardNew
	kindBoardLog
	kindBoardAssign
	kindBoardAsk
	kindCheck
	kindEdit
	kindWrite
	kindRemember
	kindSpawn
	kindAskPerson
	kindAsk
)

func Of(name string, args json.RawMessage) Card {
	var fields map[string]any
	_ = json.Unmarshal(args, &fields)
	text := func(key string) string {
		value, _ := fields[key].(string)
		return strings.Join(strings.Fields(value), " ")
	}
	card := func(family Family, kind kind, doing, verb, object string) Card {
		return Card{Family: family, kind: kind, Doing: doing, Verb: verb, Object: object, Tone: Ok, Fold: family != Asked}
	}
	switch name {
	case "bash":
		return ofCommand(text("command"))
	case "read", "scratch_read":
		return card(Explored, kindRead, "reading", "read", base(text("path")))
	case "artifact_fetch":
		return card(Explored, kindRead, "reading", "read", "a stored result")
	case "search":
		return card(Explored, kindSearch, "searching", "searched", text("pattern"))
	case "symbols", "lookup":
		return card(Explored, kindSearch, "looking up", "looked up", text("name"))
	case "recall":
		return card(Explored, kindSearch, "recalling", "recalled", text("query"))
	case "glob", "scratch_list":
		return card(Explored, kindList, "listing", "listed", text("pattern"))
	case "shell":
		if op := text("op"); op == "wait" || op == "logs" {
			return card(Explored, kindRead, "reading", "read", text("name"))
		}
		return card(Ran, kindRan, "running", text("op"), text("name"))
	case "edit":
		edit := card(Edited, kindEdit, "editing", "edited", "")
		edit.Changes = editedPaths(text("path"), fields["edits"])
		edit.Object = changedNames(edit.Changes)
		return edit
	case "write":
		write := card(Edited, kindWrite, "writing", "wrote", base(text("path")))
		write.Changes = []Change{{Path: text("path")}}
		return write
	case "test", "typecheck":
		return card(Tests, kindCheck, name+"ing", name, base(text("path")))
	case "ticket_move":
		move := card(Board, kindBoardMove, "moving", "moved", text("id"))
		move.Meta = movedTo(text("to"))
		return move
	case "ticket_create", "ticket_request":
		opened := card(Board, kindBoardNew, "opening", "opened", "")
		opened.Meta = text("title")
		return opened
	case "ticket_log":
		logged := card(Board, kindBoardLog, "logging on", "logged on", text("id"))
		logged.Meta = cmp.Or(text("kind"), "say")
		return logged
	case "ticket_assign":
		assigned := card(Board, kindBoardAssign, "assigning", "assigned", text("id"))
		assigned.Meta = "to " + text("to")
		return assigned
	case "ticket_ask":
		return card(Board, kindBoardAsk, "asking on", "asked on", text("id"))
	case "ticket_read", "ticket_list":
		return card(Board, kindRan, "reading", "read", cmp.Or(text("id"), text("status")))
	case "remember":
		return card(Memory, kindRemember, "remembering", "remembered", "")
	case "spawn":
		spawned := card(Agents, kindSpawn, "starting", "started", cmp.Or(text("agent"), "a sub-agent"))
		spawned.Meta = text("ticket")
		return spawned
	case "message":
		return card(Agents, kindRan, "messaging", messaged(text("answer"), text("do")), text("to"))
	case "subagents":
		return card(Agents, kindRan, "checking", "checked", "sub-agents")
	case "ask_person":
		return card(Asked, kindAskPerson, "asking you:", "asked you:", firstHeader(fields["questions"]))
	case "ask":
		return card(Asked, kindAsk, "asking", "asked", "the orchestrator")
	case "fetch":
		return card(Web, kindRan, "fetching", "fetched", host(text("url")))
	case "web_search":
		return card(Web, kindRan, "searching the web for", "searched", text("query"))
	}
	if strings.HasPrefix(name, "browser_") {
		return card(Web, kindRan, "using the browser", "used the browser", "")
	}
	return card(Ran, kindRan, "running", "ran", strings.ReplaceAll(name, "_", " "))
}

func (c Card) Settled(content string, failed bool, exit int) Card {
	lines, trimmed := readable(content)
	c.settled = true
	if failed || exit != 0 {
		c.Tone = Fail
	}
	switch c.kind {
	case kindRan, kindRead, kindList, kindGitAdd, kindSpawn:
	case kindSearch:
		if exit == searchFoundNone && len(lines) == 0 {
			c.Tone, c.Meta = Ok, "no matches"
		}
	case kindGitStatus:
		c.Meta = statusCount(lines)
	case kindGitDiff:
		c.Meta = diffCount(lines, trimmed)
	case kindGitLog:
		c.Meta = logCount(c.words, lines, trimmed)
	case kindCommit:
		c = committed(c, lines)
	case kindPush:
		c = pushed(c, lines)
	case kindPull:
		c = pulled(c, lines)
	case kindSwitch:
		c = switched(c, lines)
	case kindStash:
		c = stashed(c, lines)
	case kindReset:
		for _, line := range lines {
			if at, found := strings.CutPrefix(line, "HEAD is now at "); found {
				c.Object = firstWord(at)
			}
		}
	case kindBoardMove, kindBoardNew, kindBoardLog, kindBoardAssign, kindBoardAsk:
		c = receipted(c, lines)
	case kindCheck:
		c = checked(c, lines)
	case kindEdit, kindWrite:
		c = edited(c, lines, trimmed)
	case kindRemember:
		c = remembered(c, strings.Join(lines, "\n"))
	case kindAskPerson:
		c = answered(c, strings.Join(lines, "\n"))
	case kindAsk:
		c = orchestratorAnswered(c, strings.Join(lines, "\n"))
	default:
		panic("toolcard: unknown kind " + strconv.Itoa(int(c.kind)))
	}
	if said := "exit " + strconv.Itoa(exit); c.Tone == Fail && exit != 0 && c.Meta == "" && c.Object != said {
		c.Meta = said
	}
	if c.Tone == Fail && c.failedVerb != "" {
		c.Verb = c.failedVerb
	}
	c.Fold = c.Tone != Fail && c.Family != Asked
	return c
}

func editedPaths(first string, listed any) []Change {
	var changes []Change
	add := func(path string) {
		for _, change := range changes {
			if change.Path == path {
				return
			}
		}
		if path != "" {
			changes = append(changes, Change{Path: path})
		}
	}
	add(first)
	edits, _ := listed.([]any)
	for _, edit := range edits {
		fields, _ := edit.(map[string]any)
		path, _ := fields["path"].(string)
		add(path)
	}
	return changes
}

func changedNames(changes []Change) string {
	names := make([]string, len(changes))
	for i, change := range changes {
		names[i] = base(change.Path)
	}
	return strings.Join(names, ", ")
}

func firstHeader(questions any) string {
	asked, _ := questions.([]any)
	if len(asked) == 0 {
		return ""
	}
	fields, _ := asked[0].(map[string]any)
	header, _ := fields["header"].(string)
	return header
}

func messaged(answer, do string) string {
	switch {
	case answer == "allow", answer == "allow_here":
		return "allowed"
	case answer == "deny":
		return "denied"
	case do == "stop":
		return "stopped"
	case do == "kill":
		return "killed"
	case do == "release":
		return "released"
	case do == "grant":
		return "granted paths to"
	case do == "revoke":
		return "revoked paths from"
	}
	return "messaged"
}

func movedTo(status string) string {
	if status == "" {
		return ""
	}
	return "→ " + status
}

func base(file string) string {
	if file == "" {
		return ""
	}
	return path.Base(strings.ReplaceAll(strings.TrimRight(file, `/\`), `\`, "/"))
}

func host(url string) string {
	if _, rest, found := strings.Cut(url, "://"); found {
		url = rest
	}
	head, _, _ := strings.Cut(url, "/")
	head, _, _ = strings.Cut(head, "?")
	return head
}
