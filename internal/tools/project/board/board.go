package board

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"tofu/internal/agent/loop"
	"tofu/internal/konst"
	"tofu/internal/librarian/rules"
	"tofu/internal/models/llm/core"
	"tofu/internal/project/boardy"
	"tofu/internal/project/pm"
	"tofu/internal/tools/contract"
)

type boardVerb string

const (
	boardRule      boardVerb = "board_rule"
	boardEpic      boardVerb = "epic"
	boardSprint    boardVerb = "sprint"
	boardMilestone boardVerb = "milestone"
	boardTriage    boardVerb = "triage"
	boardRequest   boardVerb = "request"
	boardCreate    boardVerb = "board_create"
)

type boardTool struct {
	verb    boardVerb
	tracker pm.Tracker
	actor   string
}

type boardArgs struct {
	Board, Action, ID, Title, Source, Scope, Ticket, To, Milestone, Start, End, Due, Request, Reason, Epic, Sprint, Note, Design, First, Words, Verdict string
	Tickets                                                                                                                                             []string
}

func Tools(tracker pm.Tracker, actor string) []contract.Tool {
	var list []contract.Tool
	for _, verb := range []boardVerb{boardRule, boardEpic, boardSprint, boardMilestone, boardTriage, boardRequest} {
		list = append(list, boardTool{verb: verb, tracker: tracker, actor: actor})
	}
	return list
}

func Create(tracker pm.Tracker, actor string) contract.Tool {
	return boardTool{verb: boardCreate, tracker: tracker, actor: actor}
}

func StringParam(about string) map[string]any {
	return map[string]any{"type": "string", "description": about}
}

func Nudged(tracker pm.Tracker, name string, run func() (contract.Result, error)) (contract.Result, error) {
	sizes := tracker.EventSizes()
	result, err := run()
	note := loop.NudgesText(tracker.NudgesSince(sizes))
	if err != nil {
		return contract.Result{}, fmt.Errorf("%s: %w%s", name, err, note)
	}
	result.Content += note
	return result, nil
}

func (t boardTool) Name() string { return string(t.verb) }

func (t boardTool) Definition() core.Tool {
	properties := map[string]any{"board": StringParam("the board key, such as DEMO")}
	required := []string{"board", "action"}
	var describe string
	var actions []string
	switch t.verb {
	case boardCreate:
		describe = "makes this project's board, which it does not have yet, with the shipped board hooks, and you manage it. make it before your first edit or spawn on a task that changes code in more than one folder, that you split into pieces, or that a sub-agent works on, and tell the person in one line that you made it; a question, a read or a change to a few files in one folder needs no board"
		properties["board"] = StringParam("the new board's key: two to ten capital letters or digits from the project's name, starting with a letter, such as LANG for project-lang")
		properties["title"], required = StringParam("the board's name, such as the project's name"), []string{"board", "title"}
	case boardRule:
		describe = "reads and writes the board hooks that run on every board event, so a check you would run by hand on every ticket runs itself. list shows each hook with its scope, trigger and last fire; show prints one hook's YAML; add writes a new hook from source; edit replaces the hook source names; remove deletes one; test runs one against a ticket and writes nothing. a hook is YAML lines: id, when (before or after), on (ticket.create, ticket.move, ticket.assign, ticket.log, ticket.widen, ticket.close, epic.create, sprint.open, sprint.close, milestone.reach, triage.request, triage.accept, question.ask, question.answer, agent.spawn, agent.report, lead.attach, schedule with every 1h or daily 09:00), if: with indented conditions (from, to, type, priority, labels, actor, epic set or empty, sprint, owns, in_status_for 2h), do (a check: command, field, person or check evidenced, then refuse, nudge, ask or record; or after an event an effect: create, move, assign, log, notify, set), text, the words the actor reads, and for a refuse or ask an optional bypass: block with indented who, accept and wait. one hook runs per id, board over project over user"
		actions = []string{"list", "show", "add", "edit", "remove", "test"}
		properties["id"] = StringParam("the hook id, for show, remove and test")
		properties["source"] = StringParam("the hook's whole YAML, for add and edit, or for test of a hook not yet written")
		properties["scope"] = map[string]any{"type": "string", "enum": boardy.HookScope("").Enum(), "description": "where the hook lives: board (the default), project for every board here, user for every board the person works on"}
		properties["ticket"] = StringParam("the ticket test runs the hook against")
		properties["to"] = StringParam("the status test assumes a move goes to")
	case boardEpic:
		describe = "lists a board's epics with their milestones, makes an epic, records its research and its design, puts a ticket in one, or marks one done; every ticket belongs to one, so make the epic before the tickets that serve it. a new epic takes its first ticket only after research (what two rivals do and the options, from a research spawn) and a design naming the smallest first version that runs, and its second only once the first is done with a driven path"
		actions = []string{"list", "new", "research", "design", "add", "done"}
		properties["id"], properties["title"] = StringParam("the epic id: letters, digits and dashes"), StringParam("one line, for new")
		properties["milestone"], properties["ticket"] = StringParam("the milestone the epic reaches, for new"), StringParam("the ticket add puts in the epic")
		properties["note"], properties["design"] = StringParam("what the rivals do and the options, for research"), StringParam("the shape of the work, for design")
		properties["first"] = StringParam("the smallest first version that runs, for design")
	case boardSprint:
		describe = "lists a board's sprints, plans a new one, puts a ticket in one, starts one (closing the active one) or closes one"
		actions = []string{"list", "new", "add", "start", "close"}
		properties["id"], properties["title"] = StringParam("the sprint id: letters, digits and dashes"), StringParam("one line, for new")
		properties["start"], properties["end"] = StringParam("its first day, YYYY-MM-DD, for new"), StringParam("its last day, YYYY-MM-DD, for new")
		properties["ticket"] = StringParam("the ticket add puts in the sprint")
	case boardMilestone:
		describe = "lists a board's milestones or makes one; an epic names the milestone it reaches"
		actions = []string{"list", "new"}
		properties["id"], properties["title"] = StringParam("the milestone id: letters, digits and dashes"), StringParam("one line, for new")
		properties["due"] = StringParam("the day it is due, YYYY-MM-DD, for new")
	case boardTriage:
		describe = "lists the requests waiting in a board's triage, accepts one into the backlog as a ticket in an epic and a sprint, or drops one with a reason"
		actions = []string{"list", "accept", "drop"}
		properties["request"], properties["reason"] = StringParam("the request id, for accept and drop"), StringParam("why it is dropped")
		properties["epic"], properties["sprint"] = StringParam("the epic the accepted ticket goes in"), StringParam("the sprint the accepted ticket is planned in")
	case boardRequest:
		describe = "holds what the person asked, in their own words, with every ticket cut from it. a request is done only when every ticket is done and a review against the words passes; a ticket moved after the review asks for a new one. say done to the person only for a request whose state is done, never for a ticket, and when part of the ask is left, cut it as its own ticket, add it here, and say the request is open. new quotes the words verbatim with the tickets; add puts a later ticket in; review records pass or fail with a note on how the work holds against the words, once every ticket is done; show and list say each request's state"
		actions = []string{"list", "show", "new", "add", "review"}
		properties["id"], properties["words"] = StringParam("the request id, such as R1, for show, add and review"), StringParam("the person's words exactly as written, for new")
		properties["tickets"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "the ticket ids cut from it, for new and add"}
		properties["verdict"] = map[string]any{"type": "string", "enum": []string{string(boardy.ReviewPass), string(boardy.ReviewFail)}, "description": "the review's verdict"}
		properties["note"] = StringParam("how the work holds against the words, for review")
	}
	if actions != nil {
		properties["action"] = map[string]any{"type": "string", "enum": actions}
	}
	return core.Tool{Name: string(t.verb), Description: describe, Parameters: map[string]any{"type": "object", "properties": properties, "required": required}}
}

func (t boardTool) Run(_ context.Context, raw json.RawMessage) (contract.Result, error) {
	var args boardArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return contract.Result{}, fmt.Errorf("%s: arguments are not the expected shape: %w", t.verb, err)
	}
	return Nudged(t.tracker, string(t.verb), func() (contract.Result, error) {
		var said string
		var err error
		switch t.verb {
		case boardCreate:
			said, err = t.create(args)
		case boardRule:
			said, err = t.rule(args)
		case boardEpic:
			said, err = t.epic(args)
		case boardSprint:
			said, err = t.sprint(args)
		case boardMilestone:
			said, err = t.milestone(args)
		case boardTriage:
			said, err = t.triage(args)
		case boardRequest:
			said, err = t.request(args)
		}
		return contract.Result{Content: said, Command: strings.Join(strings.Fields(string(t.verb)+" "+args.Action+" "+args.Board+" "+args.ID), " ")}, err
	})
}

func hookLine(row boardy.HookRow) string {
	line := fmt.Sprintf("%s, %s scope: %s, do %s", row.ID, row.Scope, row.Trigger, strings.TrimSpace(string(row.Do)+" "+row.Arg))
	if row.Then != "" {
		line += ", then " + string(row.Then)
	}
	if !row.LastFired.IsZero() {
		line += ", last fired " + row.LastFired.Format(time.DateTime)
	}
	if row.ShadowedBy != "" {
		line += ", shadowed by the " + string(row.ShadowedBy) + " hook of the same id, so it does not run"
	}
	if row.Bypassed > 0 {
		line += fmt.Sprintf(", flagged: bypassed %d times in %d days", row.Bypassed, konst.BoardBypassFlagDays)
	}
	return line
}

func (t boardTool) rule(a boardArgs) (string, error) {
	scope := boardy.HookScope(a.Scope)
	switch a.Action {
	case "list":
		listed, err := t.tracker.Rules(boardy.RulesRequest{Board: a.Board})
		lines := []string{fmt.Sprintf("%s has %d board hooks", listed.Board, len(listed.Hooks))}
		for _, row := range listed.Hooks {
			lines = append(lines, "- "+hookLine(row))
		}
		return strings.Join(lines, "\n"), err
	case "show":
		got, err := t.tracker.RuleGet(boardy.RuleRequest{Board: a.Board, Scope: scope, ID: a.ID})
		return hookLine(got.Hook) + "\n" + got.Source, err
	case "add", "edit":
		h, err := rules.ParseHook([]byte(a.Source), "source")
		if err != nil {
			return "", err
		}
		_, missing := t.tracker.RuleGet(boardy.RuleRequest{Board: a.Board, Scope: scope, ID: h.ID})
		if a.Action == "add" && missing == nil {
			return "", fmt.Errorf("%s already has the hook %s, and edit replaces it", a.Board, h.ID)
		}
		if a.Action == "edit" && missing != nil {
			return "", missing
		}
		put, err := t.tracker.RulePut(boardy.RulePutRequest{Board: a.Board, Scope: scope, Source: a.Source}, t.actor)
		return "wrote " + hookLine(put.Hook) + "; board_rule remove " + put.Hook.ID + " takes it out", err
	case "remove":
		removed, err := t.tracker.RuleRemove(boardy.RuleRequest{Board: a.Board, Scope: scope, ID: a.ID}, t.actor)
		said := "removed " + removed.Hook.ID + " from " + removed.Hook.File
		if next := removed.TakesOver; next != nil {
			said += "; the " + string(next.Scope) + " hook " + hookLine(*next) + " takes over"
		}
		return said, err
	case "test":
		tested, err := t.tracker.RuleTest(boardy.RuleTestRequest{Board: a.Board, Scope: scope, ID: a.ID, Source: a.Source, Ticket: a.Ticket, To: boardy.Status(a.To)}, t.actor)
		return strings.TrimSpace(tested.Hook.ID + " on " + cmp.Or(tested.Ticket, a.Board) + ": " + tested.Verdict + "\n" + tested.Detail), err
	}
	return "", unknownAction(a.Action, "list, show, add, edit, remove or test")
}

func unknownAction(action, known string) error {
	return fmt.Errorf("action %q is %s", action, known)
}

func (t boardTool) manages(key string) error {
	return t.tracker.Manages(key, t.actor, "change its epics, sprints and milestones")
}

func (t boardTool) create(a boardArgs) (string, error) {
	boards, err := t.tracker.Boards()
	if err != nil {
		return "", err
	}
	if len(boards) > 0 {
		return "", fmt.Errorf("this project already has the board %s, so work it with ticket_list and ticket_create", boards[0].Key)
	}
	if err := t.tracker.Start(a.Board, a.Title); err != nil {
		return "", err
	}
	return "made the board " + a.Board + " and you manage it: tell the person in one line, then put each piece of work on it with ticket_create, with its owns and acceptance, and spawn for it with its id in ticket", nil
}

func day(raw string) (boardy.Day, error) {
	if raw == "" {
		return "", nil
	}
	at, err := time.Parse(time.DateOnly, raw)
	return boardy.DayOf(at), err
}

func (t boardTool) epic(a boardArgs) (string, error) {
	epics, err := t.tracker.Epics(a.Board)
	switch a.Action {
	case "list":
		lines := []string{fmt.Sprintf("%s has %d epics", a.Board, len(epics))}
		for _, epic := range epics {
			state := "open"
			if epic.Done {
				state = "done"
			}
			lines = append(lines, strings.TrimSpace(fmt.Sprintf("- %s %s, %s %s", epic.ID, epic.Title, state, epic.Milestone)))
		}
		return strings.Join(lines, "\n"), err
	case "new":
		if err := t.manages(a.Board); err != nil {
			return "", err
		}
		return "made the epic " + a.ID + ": record its research and its design before its first ticket", t.tracker.SaveEpic(a.Board, boardy.Epic{ID: a.ID, Title: a.Title, Milestone: a.Milestone})
	case "research", "design":
		if err := t.manages(a.Board); err != nil {
			return "", err
		}
		if a.Action == "research" {
			_, err = t.tracker.Research(a.Board, a.ID, a.Note)
		} else {
			_, err = t.tracker.Design(a.Board, a.ID, a.Design, a.First)
		}
		return a.ID + " has its " + a.Action, err
	case "add":
		_, err := t.tracker.Plan(a.Ticket, a.ID, "", t.actor)
		return a.Ticket + " is in the epic " + a.ID, err
	case "done":
		if err := t.manages(a.Board); err != nil {
			return "", err
		}
		at := slices.IndexFunc(epics, func(e boardy.Epic) bool { return e.ID == a.ID })
		if at < 0 {
			return "", fmt.Errorf("%s has no epic %s", a.Board, a.ID)
		}
		epics[at].Done = true
		return a.ID + " is done", t.tracker.SaveEpic(a.Board, epics[at])
	}
	return "", unknownAction(a.Action, "list, new, research, design, add or done")
}

func (t boardTool) sprint(a boardArgs) (string, error) {
	if a.Action == "list" {
		sprints, err := t.tracker.Sprints(a.Board)
		lines := []string{fmt.Sprintf("%s has %d sprints", a.Board, len(sprints))}
		for _, sprint := range sprints {
			lines = append(lines, strings.TrimSpace(fmt.Sprintf("- %s %s, %s %s %s", sprint.ID, sprint.Title, sprint.State, sprint.Start, sprint.End)))
		}
		return strings.Join(lines, "\n"), err
	}
	if err := t.manages(a.Board); err != nil {
		return "", err
	}
	switch a.Action {
	case "new":
		start, startErr := day(a.Start)
		end, endErr := day(a.End)
		if startErr != nil || endErr != nil {
			return "", fmt.Errorf("a sprint's start and end are days, YYYY-MM-DD: %q, %q", a.Start, a.End)
		}
		return "planned the sprint " + a.ID, t.tracker.SaveSprint(a.Board, boardy.Sprint{ID: a.ID, Title: a.Title, State: boardy.Planned, Start: start, End: end})
	case "add":
		_, err := t.tracker.Plan(a.Ticket, "", a.ID, t.actor)
		return a.Ticket + " is in the sprint " + a.ID, err
	case "start":
		return a.ID + " is active", t.tracker.SetSprint(a.Board, a.ID, boardy.Active)
	case "close":
		return a.ID + " is closed", t.tracker.SetSprint(a.Board, a.ID, boardy.Closed)
	}
	return "", unknownAction(a.Action, "list, new, add, start or close")
}

func (t boardTool) milestone(a boardArgs) (string, error) {
	switch a.Action {
	case "list":
		milestones, err := t.tracker.Milestones(a.Board)
		lines := []string{fmt.Sprintf("%s has %d milestones", a.Board, len(milestones))}
		for _, milestone := range milestones {
			lines = append(lines, strings.TrimSpace(fmt.Sprintf("- %s %s %s", milestone.ID, milestone.Title, milestone.Due)))
		}
		return strings.Join(lines, "\n"), err
	case "new":
		if err := t.manages(a.Board); err != nil {
			return "", err
		}
		due, err := day(a.Due)
		if err != nil {
			return "", fmt.Errorf("a milestone's due is a day, YYYY-MM-DD: %q", a.Due)
		}
		return "made the milestone " + a.ID, t.tracker.SaveMilestone(a.Board, boardy.Milestone{ID: a.ID, Title: a.Title, Due: due})
	}
	return "", unknownAction(a.Action, "list or new")
}

func (t boardTool) triage(a boardArgs) (string, error) {
	answer, err := t.tracker.Triage(boardy.TriageRequest{Board: a.Board, Action: boardy.TriageAction(a.Action), Request: a.Request, Reason: a.Reason, Epic: a.Epic, Sprint: a.Sprint}, t.actor)
	var lines []string
	switch {
	case answer.Accepted != nil && answer.Accepted.ID != "":
		lines = append(lines, "accepted "+a.Request+" as "+answer.Accepted.ID)
	case a.Action == string(boardy.TriageDrop) && err == nil:
		lines = append(lines, "dropped "+a.Request)
	}
	lines = append(lines, fmt.Sprintf("%s triage holds %d requests", answer.Board, len(answer.Requests)))
	for _, request := range answer.Requests {
		lines = append(lines, fmt.Sprintf("- %s by %s: %s", request.ID, cmp.Or(request.By.ID, string(request.By.Kind)), request.Ticket.Title))
	}
	return strings.Join(lines, "\n"), err
}

func standingLine(s boardy.RequestStanding) string {
	return s.Request.ID + " is " + string(s.State) + ": " + s.Why
}

func (t boardTool) request(a boardArgs) (string, error) {
	var standing boardy.RequestStanding
	var err error
	switch a.Action {
	case "list":
		standings, err := t.tracker.RequestStandings(a.Board)
		lines := []string{fmt.Sprintf("%s has %d requests", a.Board, len(standings))}
		for _, s := range standings {
			lines = append(lines, "- "+standingLine(s))
		}
		return strings.Join(lines, "\n"), err
	case "show":
		standing, err = t.tracker.RequestStanding(a.Board, a.ID)
	case "new":
		standing, err = t.tracker.NewRequest(a.Board, a.Words, a.Tickets, t.actor)
	case "add":
		standing, err = t.tracker.AddToRequest(a.Board, a.ID, a.Tickets, t.actor)
	case "review":
		var verdict boardy.ReviewVerdict
		if verdict, err = boardy.ParseReviewVerdict(a.Verdict); err == nil {
			standing, err = t.tracker.ReviewRequest(a.Board, a.ID, verdict, a.Note, t.actor)
		}
	default:
		return "", unknownAction(a.Action, "list, show, new, add or review")
	}
	if err != nil {
		return "", err
	}
	lines := []string{standingLine(standing), "words: " + standing.Request.Words}
	for _, ticket := range standing.Tickets {
		lines = append(lines, "- "+ticket.ID+" "+string(ticket.Status)+" "+ticket.Title)
	}
	return strings.Join(lines, "\n"), nil
}
