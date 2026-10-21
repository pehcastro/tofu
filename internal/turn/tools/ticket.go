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
	"tofu/internal/konst"
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
	ticketList    ticketVerb = "ticket_list"
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
		verbs = append(verbs, ticketCreate, ticketAssign, ticketList)
	}
	list := make([]turn.Tool, len(verbs))
	for i, verb := range verbs {
		list[i] = ticketTool{verb: verb, board: board, actor: actor, manager: manager}
	}
	if manager {
		for _, verb := range []boardVerb{boardRule, boardEpic, boardSprint, boardMilestone, boardTriage} {
			list = append(list, boardTool{verb: verb, board: board, actor: actor})
		}
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
	describe, properties, required := "", map[string]any{"id": stringParam("the ticket id, such as DEMO-2")}, []string{"id"}
	switch t.verb {
	case ticketRead:
		describe = "reads a ticket by its id from any board of this project: front matter, Problem, Scope, every Acceptance revision (the last one is in force) and Log"
	case ticketLog:
		describe = "appends one line of evidence to the Log of your own ticket: what you ran and what it printed"
		properties["text"], required = stringParam("the evidence line"), append(required, "text")
	case ticketAsk:
		describe = "puts a question to the board's managers by writing it into your own ticket's Log; a manager answers there, so read the ticket again with ticket_read before you act on the answer"
		properties["question"], required = stringParam("the question"), append(required, "question")
		if t.manager {
			describe = "puts a decision to the person on a ticket of a board you manage: it opens a question in the ticket's Questions, which the person answers in catch up, and lets the ticket move to needs-decision"
			properties["question"] = stringParam("the tldr: one line saying what is to be decided")
			properties["eli5"] = stringParam("one to three plain lines explaining it to someone who has not read the code")
			properties["options"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "two to four options, one line each"}
			properties["recommend"] = map[string]any{"type": "integer", "description": "the option you recommend, counted from 1"}
			properties["why"] = stringParam("one line saying why you recommend it")
			required = append(required, "eli5", "options", "recommend", "why")
		}
	case ticketRequest, ticketCreate:
		describe = "writes a request for a new ticket into the triage of a board, where a manager accepts or drops it"
		properties = map[string]any{
			"board": stringParam("the board key, such as DEMO"), "title": stringParam("one line"), "problem": stringParam("why the work is needed"),
			"scope": stringParam("what is in and out"), "acceptance": stringParam("acceptance lines, one per line, each starting with - "),
			"owns":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "the path globs the work may write"},
			"parent": stringParam("a ticket to split, such as DEMO-2: the new one becomes its sub-ticket DEMO-2-1, and its owns must sit inside the parent's"),
		}
		required = []string{"board", "title", "acceptance"}
		if t.verb == ticketCreate {
			describe = "creates a ticket on a board you manage, numbered by the board; put it in an epic and a sprint, and make them first with epic and sprint when none fits"
			properties["epic"], properties["sprint"] = stringParam("the epic id it belongs to, from epic list"), stringParam("the sprint id it is planned in, from sprint list")
		}
	case ticketMove:
		statuses := []boardy.Status{boardy.Review}
		describe = "moves your own ticket from doing to review once its Log holds the evidence for every acceptance line in force"
		if t.manager {
			statuses, describe = boardy.Statuses(), "moves a ticket on a board you manage to a status; blocked needs a reason"
		}
		properties["to"], properties["reason"], required = map[string]any{"type": "string", "enum": statuses}, stringParam("why, required for blocked"), append(required, "to")
	case ticketAssign:
		describe = "assigns a ticket on a board you manage to a session or the person, by name"
		properties["to"], required = stringParam("who the ticket goes to"), append(required, "to")
	case ticketList:
		describe = fmt.Sprintf("lists the tickets in one status across the boards of this project, oldest first, with the time since each last changed; it shows %d and counts the rest", konst.LeadCheckInReviewsMost)
		properties, required = map[string]any{"status": map[string]any{"type": "string", "enum": boardy.Statuses()}}, []string{"status"}
	}
	return llm.Tool{Name: string(t.verb), Description: describe, Parameters: map[string]any{"type": "object", "properties": properties, "required": required}}
}

func (t ticketTool) Run(_ context.Context, raw json.RawMessage) (turn.Result, error) {
	var args struct {
		ID, Text, Question, ELI5, Why, Board, Title, Problem, Scope, Acceptance, To, Reason, Parent, Epic, Sprint, Status string
		Owns, Options                                                                                                     []string
		Recommend                                                                                                         int
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return turn.Result{}, fmt.Errorf("%s: arguments are not the expected shape: %w", t.verb, err)
	}
	return nudged(t.board.Store, string(t.verb), func() (turn.Result, error) {
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
			if t.manager {
				if ticket, err = t.board.Ask(args.ID, boardy.Question{TLDR: args.Question, ELI5: args.ELI5, Options: args.Options, Recommend: args.Recommend, Why: args.Why}, t.actor); err == nil {
					said = fmt.Sprintf("asked Q%d on %s; the person answers it in catch up", ticket.Questions[len(ticket.Questions)-1].ID, ticket.ID)
				}
				break
			}
			ticket, err = t.board.AskInLog(args.ID, args.Question, t.actor)
			said = "asked in the Log of " + ticket.ID + "; a manager answers there"
		case ticketRequest, ticketCreate:
			command = string(t.verb) + " " + args.Board
			draft := boardy.Ticket{Front: boardy.Front{Title: args.Title, Owns: args.Owns, Parent: args.Parent, Epic: args.Epic, Sprint: args.Sprint}, Problem: args.Problem, Scope: args.Scope, Acceptance: []string{args.Acceptance}}
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
		case ticketList:
			command = string(t.verb) + " " + args.Status
			var status boardy.Status
			if status, err = boardy.ParseStatus(args.Status); err != nil {
				break
			}
			tickets := turn.BoardTickets(t.board.Store, status)
			lines, more := turn.Waiting(tickets, time.Now())
			for i := range lines {
				lines[i] += " " + tickets[i].Title
			}
			said = fmt.Sprintf("%d %s: %s", len(tickets), status, cmp.Or(strings.Join(lines, "; "), "none"))
			if more > 0 {
				said += fmt.Sprintf("; and %d more", more)
			}
		}
		return turn.Result{Content: said, Command: command}, err
	})
}
