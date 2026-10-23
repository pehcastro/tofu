package tools

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"tofu/internal/boardy"
	"tofu/internal/llm"
	"tofu/internal/rule"
	"tofu/internal/turn"
)

type boardVerb string

const (
	boardRule      boardVerb = "board_rule"
	boardEpic      boardVerb = "epic"
	boardSprint    boardVerb = "sprint"
	boardMilestone boardVerb = "milestone"
	boardTriage    boardVerb = "triage"
)

type boardTool struct {
	verb  boardVerb
	board boardy.Managed
	actor string
}

type boardArgs struct {
	Board, Action, ID, Title, Source, Scope, Ticket, To, Milestone, Start, End, Due, Request, Reason, Epic, Sprint string
}

func stringParam(about string) map[string]any {
	return map[string]any{"type": "string", "description": about}
}

func nudged(store boardy.Store, name string, run func() (turn.Result, error)) (turn.Result, error) {
	sizes := store.EventSizes()
	result, err := run()
	note := turn.BoardNudges(store, sizes)
	if err != nil {
		return turn.Result{}, fmt.Errorf("%s: %w%s", name, err, note)
	}
	result.Content += note
	return result, nil
}

func (t boardTool) Name() string { return string(t.verb) }

func (t boardTool) Definition() llm.Tool {
	properties := map[string]any{"board": stringParam("the board key, such as DEMO")}
	var describe string
	var actions []string
	switch t.verb {
	case boardRule:
		describe = "reads and writes the board hooks that run on every board event, so a check you would run by hand on every ticket runs itself. list shows each hook with its scope, trigger and last fire; show prints one hook's YAML; add writes a new hook from source; edit replaces the hook source names; remove deletes one; test runs one against a ticket and writes nothing. a hook is YAML lines: id, when (before or after), on (ticket.create, ticket.move, ticket.assign, ticket.log, ticket.widen, ticket.close, epic.create, sprint.open, sprint.close, milestone.reach, triage.request, triage.accept, question.ask, question.answer, agent.spawn, agent.report, lead.attach, schedule with every 1h or daily 09:00), if: with indented conditions (from, to, type, priority, labels, actor, epic set or empty, sprint, owns, in_status_for 2h), do (a check: command, field, person, then refuse, nudge, ask or record; or after an event an effect: create, move, assign, log, notify, set) and text, the words the actor reads"
		actions = []string{"list", "show", "add", "edit", "remove", "test"}
		properties["id"] = stringParam("the hook id, for show, remove and test")
		properties["source"] = stringParam("the hook's whole YAML, for add and edit, or for test of a hook not yet written")
		properties["scope"] = map[string]any{"type": "string", "enum": boardy.HookScope("").Enum(), "description": "where the hook lives: board (the default), project for every board here, user for every board the person works on"}
		properties["ticket"] = stringParam("the ticket test runs the hook against")
		properties["to"] = stringParam("the status test assumes a move goes to")
	case boardEpic:
		describe = "lists a board's epics with their milestones, makes an epic, puts a ticket in one, or marks one done; every ticket belongs to one, so make the epic before the tickets that serve it"
		actions = []string{"list", "new", "add", "done"}
		properties["id"], properties["title"] = stringParam("the epic id: letters, digits and dashes"), stringParam("one line, for new")
		properties["milestone"], properties["ticket"] = stringParam("the milestone the epic reaches, for new"), stringParam("the ticket add puts in the epic")
	case boardSprint:
		describe = "lists a board's sprints, plans a new one, puts a ticket in one, starts one (closing the active one) or closes one"
		actions = []string{"list", "new", "add", "start", "close"}
		properties["id"], properties["title"] = stringParam("the sprint id: letters, digits and dashes"), stringParam("one line, for new")
		properties["start"], properties["end"] = stringParam("its first day, YYYY-MM-DD, for new"), stringParam("its last day, YYYY-MM-DD, for new")
		properties["ticket"] = stringParam("the ticket add puts in the sprint")
	case boardMilestone:
		describe = "lists a board's milestones or makes one; an epic names the milestone it reaches"
		actions = []string{"list", "new"}
		properties["id"], properties["title"] = stringParam("the milestone id: letters, digits and dashes"), stringParam("one line, for new")
		properties["due"] = stringParam("the day it is due, YYYY-MM-DD, for new")
	case boardTriage:
		describe = "lists the requests waiting in a board's triage, accepts one into the backlog as a ticket in an epic and a sprint, or drops one with a reason"
		actions = []string{"list", "accept", "drop"}
		properties["request"], properties["reason"] = stringParam("the request id, for accept and drop"), stringParam("why it is dropped")
		properties["epic"], properties["sprint"] = stringParam("the epic the accepted ticket goes in"), stringParam("the sprint the accepted ticket is planned in")
	}
	properties["action"] = map[string]any{"type": "string", "enum": actions}
	return llm.Tool{Name: string(t.verb), Description: describe, Parameters: map[string]any{"type": "object", "properties": properties, "required": []string{"board", "action"}}}
}

func (t boardTool) Run(_ context.Context, raw json.RawMessage) (turn.Result, error) {
	var args boardArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return turn.Result{}, fmt.Errorf("%s: arguments are not the expected shape: %w", t.verb, err)
	}
	return nudged(t.board.Store, string(t.verb), func() (turn.Result, error) {
		var said string
		var err error
		switch t.verb {
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
		}
		return turn.Result{Content: said, Command: strings.TrimSpace(string(t.verb) + " " + args.Action + " " + args.Board + " " + args.ID)}, err
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
	return line
}

func (t boardTool) rule(a boardArgs) (string, error) {
	api := boardy.API{Board: t.board, Actor: t.actor}
	scope := boardy.HookScope(a.Scope)
	switch a.Action {
	case "list":
		listed, err := api.Rules(boardy.RulesRequest{Board: a.Board})
		lines := []string{fmt.Sprintf("%s has %d board hooks", listed.Board, len(listed.Hooks))}
		for _, row := range listed.Hooks {
			lines = append(lines, "- "+hookLine(row))
		}
		return strings.Join(lines, "\n"), err
	case "show":
		got, err := api.RuleGet(boardy.RuleRequest{Board: a.Board, Scope: scope, ID: a.ID})
		return hookLine(got.Hook) + "\n" + got.Source, err
	case "add", "edit":
		h, err := rule.ParseHook([]byte(a.Source), "source")
		if err != nil {
			return "", err
		}
		_, missing := api.RuleGet(boardy.RuleRequest{Board: a.Board, Scope: scope, ID: h.ID})
		if a.Action == "add" && missing == nil {
			return "", fmt.Errorf("%s already has the hook %s, and edit replaces it", a.Board, h.ID)
		}
		if a.Action == "edit" && missing != nil {
			return "", missing
		}
		put, err := api.RulePut(boardy.RulePutRequest{Board: a.Board, Scope: scope, Source: a.Source})
		return "wrote " + hookLine(put.Hook) + "; board_rule remove " + put.Hook.ID + " takes it out", err
	case "remove":
		removed, err := api.RuleRemove(boardy.RuleRequest{Board: a.Board, Scope: scope, ID: a.ID})
		return "removed " + removed.Hook.ID + " from " + removed.Hook.File, err
	case "test":
		tested, err := api.RuleTest(boardy.RuleTestRequest{Board: a.Board, Scope: scope, ID: a.ID, Source: a.Source, Ticket: a.Ticket, To: boardy.Status(a.To)})
		return strings.TrimSpace(tested.Hook.ID + " on " + cmp.Or(tested.Ticket, a.Board) + ": " + tested.Verdict + "\n" + tested.Detail), err
	}
	return "", unknownAction(a.Action, "list, show, add, edit, remove or test")
}

func unknownAction(action, known string) error {
	return fmt.Errorf("action %q is %s", action, known)
}

func (t boardTool) manages(key string) error {
	return t.board.Manages(key, t.actor, "change its epics, sprints and milestones")
}

func day(raw string) (boardy.Day, error) {
	if raw == "" {
		return "", nil
	}
	at, err := time.Parse(time.DateOnly, raw)
	return boardy.DayOf(at), err
}

func (t boardTool) epic(a boardArgs) (string, error) {
	store := t.board.Store
	epics, err := store.Epics(a.Board)
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
		return "made the epic " + a.ID, store.SaveEpic(a.Board, boardy.Epic{ID: a.ID, Title: a.Title, Milestone: a.Milestone})
	case "add":
		_, err := t.board.Plan(a.Ticket, a.ID, "", t.actor)
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
		return a.ID + " is done", store.SaveEpic(a.Board, epics[at])
	}
	return "", unknownAction(a.Action, "list, new, add or done")
}

func (t boardTool) sprint(a boardArgs) (string, error) {
	store := t.board.Store
	if a.Action == "list" {
		sprints, err := store.Sprints(a.Board)
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
		return "planned the sprint " + a.ID, store.SaveSprint(a.Board, boardy.Sprint{ID: a.ID, Title: a.Title, State: boardy.Planned, Start: start, End: end})
	case "add":
		_, err := t.board.Plan(a.Ticket, "", a.ID, t.actor)
		return a.Ticket + " is in the sprint " + a.ID, err
	case "start":
		return a.ID + " is active", store.SetSprint(a.Board, a.ID, boardy.Active)
	case "close":
		return a.ID + " is closed", store.SetSprint(a.Board, a.ID, boardy.Closed)
	}
	return "", unknownAction(a.Action, "list, new, add, start or close")
}

func (t boardTool) milestone(a boardArgs) (string, error) {
	store := t.board.Store
	switch a.Action {
	case "list":
		milestones, err := store.Milestones(a.Board)
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
		return "made the milestone " + a.ID, store.SaveMilestone(a.Board, boardy.Milestone{ID: a.ID, Title: a.Title, Due: due})
	}
	return "", unknownAction(a.Action, "list or new")
}

func (t boardTool) triage(a boardArgs) (string, error) {
	answer, err := boardy.API{Board: t.board, Actor: t.actor}.Triage(boardy.TriageRequest{Board: a.Board, Action: boardy.TriageAction(a.Action), Request: a.Request, Reason: a.Reason, Epic: a.Epic, Sprint: a.Sprint})
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
