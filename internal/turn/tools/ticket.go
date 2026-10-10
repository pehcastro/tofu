package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"tofu/internal/boardy"
	"tofu/internal/llm"
	"tofu/internal/turn"
)

type ticketVerb string

const (
	ticketRead    ticketVerb = "ticket_read"
	ticketLog     ticketVerb = "ticket_log"
	ticketAsk     ticketVerb = "ticket_ask"
	ticketRequest ticketVerb = "ticket_request"
	ticketCreate  ticketVerb = "ticket_create"
	ticketMove    ticketVerb = "ticket_move"
	ticketAssign  ticketVerb = "ticket_assign"
)

type ticketTool struct {
	verb    ticketVerb
	board   boardy.Managed
	actor   string
	manager bool
}

func TicketTools(board boardy.Managed, actor string) ([]turn.Tool, error) {
	boards, err := board.Store.Boards()
	if err != nil {
		return nil, err
	}
	manager := slices.ContainsFunc(boards, func(b boardy.Board) bool { return board.Manages(b.Key, actor, "") == nil })
	verbs := []ticketVerb{ticketRead, ticketLog, ticketAsk, ticketRequest, ticketMove}
	if manager {
		verbs = append(verbs, ticketCreate, ticketAssign)
	}
	list := make([]turn.Tool, len(verbs))
	for i, verb := range verbs {
		list[i] = ticketTool{verb: verb, board: board, actor: actor, manager: manager}
	}
	return list, nil
}

func TicketGrant(board boardy.Managed, id, lead, agent string) ([]string, error) {
	ticket, err := board.Get(id)
	if err != nil {
		return nil, err
	}
	if len(ticket.Owns) == 0 {
		return nil, fmt.Errorf("%s owns no paths, so a sub-agent spawned for it could write nothing: widen it first with tofu boardy widen", id)
	}
	if _, err := board.Assign(id, agent, lead); err != nil {
		return nil, err
	}
	if ticket.Status != boardy.Doing {
		if _, err := board.Move(id, boardy.Doing, "", lead); err != nil {
			return nil, err
		}
	}
	return ticket.Owns, nil
}

func (t ticketTool) Name() string { return string(t.verb) }

func (t ticketTool) Definition() llm.Tool {
	text := func(about string) map[string]any { return map[string]any{"type": "string", "description": about} }
	describe, properties, required := "", map[string]any{"id": text("the ticket id, such as DEMO-2")}, []string{"id"}
	switch t.verb {
	case ticketRead:
		describe = "reads a ticket by its id from any board of this project: front matter, Problem, Scope, every Acceptance revision (the last one is in force) and Log"
	case ticketLog:
		describe = "appends one line of evidence to the Log of your own ticket: what you ran and what it printed"
		properties["text"], required = text("the evidence line"), append(required, "text")
	case ticketAsk:
		describe = "puts a question to the board's managers by writing it into your own ticket's Log; a manager answers there, so read the ticket again with ticket_read before you act on the answer"
		properties["question"], required = text("the question"), append(required, "question")
	case ticketRequest, ticketCreate:
		describe = "writes a request for a new ticket into the triage of a board, where a manager accepts or drops it"
		if t.verb == ticketCreate {
			describe = "creates a ticket on a board you manage, numbered by the board"
		}
		properties = map[string]any{
			"board": text("the board key, such as DEMO"), "title": text("one line"), "problem": text("why the work is needed"),
			"scope": text("what is in and out"), "acceptance": text("acceptance lines, one per line, each starting with - "),
			"owns": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "the path globs the work may write"},
		}
		required = []string{"board", "title", "acceptance"}
	case ticketMove:
		statuses := []boardy.Status{boardy.Review}
		describe = "moves your own ticket from doing to review once its Log holds the evidence for every acceptance line in force"
		if t.manager {
			statuses, describe = boardy.Statuses(), "moves a ticket on a board you manage to a status; blocked needs a reason"
		}
		properties["to"], properties["reason"], required = map[string]any{"type": "string", "enum": statuses}, text("why, required for blocked"), append(required, "to")
	case ticketAssign:
		describe = "assigns a ticket on a board you manage to a session or the person, by name"
		properties["to"], required = text("who the ticket goes to"), append(required, "to")
	}
	return llm.Tool{Name: string(t.verb), Description: describe, Parameters: map[string]any{"type": "object", "properties": properties, "required": required}}
}

func (t ticketTool) Run(_ context.Context, raw json.RawMessage) (turn.Result, error) {
	var args struct {
		ID, Text, Question, Board, Title, Problem, Scope, Acceptance, To, Reason string
		Owns                                                                     []string
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return turn.Result{}, fmt.Errorf("%s: arguments are not the expected shape: %w", t.verb, err)
	}
	command := string(t.verb) + " " + args.ID
	var ticket boardy.Ticket
	var err error
	var said string
	switch t.verb {
	case ticketRead:
		if ticket, err = t.board.Get(args.ID); err == nil {
			said = string(ticket.Markdown())
		}
	case ticketLog:
		ticket, err = t.board.Comment(args.ID, args.Text, t.actor)
		said = "logged on " + ticket.ID
	case ticketAsk:
		ticket, err = t.board.Comment(args.ID, "question for a manager: "+args.Question, t.actor)
		said = "asked in the Log of " + ticket.ID + "; a manager answers there"
	case ticketRequest, ticketCreate:
		command = string(t.verb) + " " + args.Board
		draft := boardy.Ticket{Front: boardy.Front{Title: args.Title, Owns: args.Owns}, Problem: args.Problem, Scope: args.Scope, Acceptance: []string{args.Acceptance}}
		if ticket, err = t.board.Create(args.Board, draft, t.actor); ticket.ID == "" {
			said = ticket.Reason
		} else {
			said = "created " + ticket.ID + ", " + string(ticket.Status)
		}
	case ticketMove:
		var to boardy.Status
		if to, err = boardy.ParseStatus(args.To); err == nil {
			ticket, err = t.board.Move(args.ID, to, args.Reason, t.actor)
		}
		said = ticket.ID + " is " + string(ticket.Status)
	case ticketAssign:
		ticket, err = t.board.Assign(args.ID, args.To, t.actor)
		said = ticket.ID + " is assigned to " + ticket.Assignee
	}
	if err != nil {
		return turn.Result{}, fmt.Errorf("%s: %w", t.verb, err)
	}
	return turn.Result{Content: said, Command: command}, nil
}
