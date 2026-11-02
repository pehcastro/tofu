package tools

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"tofu/internal/boardy"
	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/session"
	"tofu/internal/turn"
)

type ticketVerb string

const (
	ticketRead        ticketVerb = "ticket_read"
	ticketLog         ticketVerb = turn.TicketLogToolName
	ticketAsk         ticketVerb = "ticket_ask"
	ticketRequest     ticketVerb = "ticket_request"
	ticketCreate      ticketVerb = "ticket_create"
	ticketMove        ticketVerb = turn.TicketMoveToolName
	ticketNotPossible ticketVerb = turn.TicketNotPossibleToolName
	ticketAssign      ticketVerb = "ticket_assign"
	ticketList        ticketVerb = "ticket_list"
)

type logKind string

const (
	logSay      logKind = "say"
	logEvidence logKind = logKind(boardy.Evidence)
	logBefore   logKind = logKind(boardy.Captured)
)

func sentencesIn(words []string) int {
	sentences := 1
	for i := 1; i < len(words); i++ {
		next, _ := utf8.DecodeRuneInString(words[i])
		if strings.ContainsAny(words[i-1][len(words[i-1])-1:], ".!?") && unicode.IsUpper(next) {
			sentences++
		}
	}
	return sentences
}

func sayable(text string) error {
	words := strings.Fields(text)
	switch sentences := sentencesIn(words); {
	case strings.Contains(strings.TrimSpace(text), "\n"):
		return fmt.Errorf("a said entry is one line: put the command and its output in an entry of kind %s", logEvidence)
	case sentences > konst.BoardSaidSentencesMost || len(words) > konst.BoardSaidWordsMost:
		return fmt.Errorf("a said entry is at most %d sentences and %d words, and this one is %d sentences and %d words: say what you decided, found or need, and put the proof in an entry of kind %s", konst.BoardSaidSentencesMost, konst.BoardSaidWordsMost, sentences, len(words), logEvidence)
	}
	return nil
}

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
	} else {
		verbs = append(verbs, ticketNotPossible)
	}
	list := make([]turn.Tool, len(verbs))
	for i, verb := range verbs {
		list[i] = ticketTool{verb: verb, board: board, actor: actor, manager: manager}
	}
	if manager {
		for _, verb := range []boardVerb{boardRule, boardEpic, boardSprint, boardMilestone, boardTriage, boardRequest} {
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

func (t ticketTool) spokenBoard() boardy.Managed {
	seeded := t.board.Store.Speakers[t.actor]
	if seeded.Handle != "" || t.board.Store.Root == "" {
		return t.board
	}
	sessions, err := session.OpenIn(t.board.Store.Root)
	if err != nil {
		return t.board
	}
	lead, found := turn.PMAuthor(sessions, t.actor, seeded.Model)
	if !found {
		return t.board
	}
	board := t.board
	board.Store.Speakers = map[string]boardy.Author{t.actor: *lead}
	return board
}

func (t ticketTool) Name() string { return string(t.verb) }

func (t ticketTool) Definition() llm.Tool {
	describe, properties, required := "", map[string]any{"id": stringParam("the ticket id, such as DEMO-2")}, []string{"id"}
	switch t.verb {
	case ticketRead:
		describe = "reads a ticket by its id from any board of this project: front matter, Problem, Scope, every Acceptance revision (the last one is in force) and Log"
	case ticketLog:
		describe = fmt.Sprintf("adds one entry to your own ticket, under your name, so never start it with your name. kind say is you talking to the lead in the first person, at most %d sentences and %d words on one line and no command output: a decision and why, a finding that changes the plan, what stops you, what you need, or that you are done. kind evidence proves one acceptance line, named by line: the command exactly as you ran it and the output lines that matter; the lead reads it at review and it stays out of the conversation. kind before captures how things work now, before you change anything: the same steps as the driven path and what they printed; a ticket that changes behaviour reaches done only with one, and one logged after an evidence entry or after a commit touching the ticket's owns is refused as late. your last say entry is where you ended, and the lead reads it as your report: say it plainly, with no heading and no bold label. progress nobody acts on is not an entry", konst.BoardSaidSentencesMost, konst.BoardSaidWordsMost)
		properties["kind"] = map[string]any{"type": "string", "enum": []logKind{logSay, logEvidence, logBefore}}
		properties["line"] = map[string]any{"type": "integer", "description": "for kind evidence, the acceptance line in force it proves, counted from 1"}
		properties["text"], required = stringParam("what you say, or the evidence"), append(required, "kind", "text")
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
			"labels": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "labels; " + boardy.DocsOnlyLabel + " marks a ticket that changes no behaviour, which reaches done with no before capture"},
		}
		required = []string{"board", "title", "acceptance"}
		var drafted boardy.Ticket
		for _, section := range drafted.Drafted() {
			properties[section.Field] = stringParam(section.Heading + ": " + section.Says)
			if section.Heading != boardy.FeasibilityHeading {
				required = append(required, section.Field)
			}
		}
		if t.verb == ticketCreate {
			describe = "creates a ticket on a board you manage, numbered by the board; put it in an epic and a sprint, and make them first with epic and sprint when none fits. a sub-ticket's owns sit inside its parent's owns, which are the ceiling, so a parent that owns nothing takes no sub-ticket and the new one is made without a parent; no owns glob may overlap the owns of a ticket in doing on any board, and the refusal names the glob and the ticket"
			properties["epic"], properties["sprint"] = stringParam("the epic id it belongs to, from epic list"), stringParam("the sprint id it is planned in, from sprint list")
		}
		describe += ". a ticket carries before, driven, still_works and error_case, and one asking for a visual or platform effect, such as a blur or transparency, carries feasibility too; a ticket missing one is refused naming it"
	case ticketMove:
		statuses := []boardy.Status{boardy.Review}
		describe = "moves your own ticket from doing to review once every acceptance line in force has an entry of kind evidence naming it, or is in leave; a board may also want a least amount of evidence, and the refusal says how much"
		if t.manager {
			statuses, describe = boardy.Statuses(), "moves a ticket on a board you manage to a status; blocked needs a reason. a ticket reaches doing, review or done only with Acceptance, review only with a Log entry, done only when every acceptance line in force has evidence naming it, it has an entry of kind before unless it is labelled docs-only, and the reason names nothing still open, and a parent reaches review or done only once its sub-tickets are done or dropped. a ticket moving into doing may not own a path a ticket in doing already owns; the refusal names the glob and the ticket. a ticket a sub-agent reported not possible stays in needs-decision until the person answers its question, and no move of yours takes it out"
		}
		properties["to"], properties["reason"], required = map[string]any{"type": "string", "enum": statuses}, stringParam("why, required for blocked"), append(required, "to")
		describe += ". a reason is one sentence on one line, and anything longer goes in ticket_log"
		properties["leave"] = map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "description": "on a move to review, the acceptance lines, counted from 1, that are not yours to prove and that the lead proves"}
		properties["bypass"] = stringParam("the id of a board hook refusing this move whose refusal names a bypass you may ask; the hook's accept decides whether it passes now, waits for an answer, or is reviewed after")
		properties["why"] = stringParam(fmt.Sprintf("with bypass, one line of at most %d characters saying why the hook does not fit this move", konst.BoardBypassWhyRunesMost))
	case ticketNotPossible:
		describe = "reports that your own ticket cannot be done as asked, which is your status for this work beside done: the reason and the options go to the person as a question, the ticket moves to needs-decision, and nobody but the person moves it on, the lead included. use it when the platform, the library or the code does not give what the ticket asks, never for work that is only hard, and then end your work"
		properties["reason"] = stringParam(fmt.Sprintf("one to %d plain lines: what cannot be done as asked and how you know, such as a vendor doc, an upstream issue or a probe you ran", konst.BoardELI5LinesMost))
		properties["options"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": fmt.Sprintf("%d to %d ways the person may go, one line each, such as the nearest thing that can be done or dropping the ask", konst.AskPersonOptionsLeast, konst.AskPersonOptionsMost)}
		properties["recommend"] = map[string]any{"type": "integer", "description": "the option you recommend, counted from 1"}
		properties["why"] = stringParam("one line saying why you recommend it")
		required = append(required, "reason", "options", "recommend", "why")
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
		Before, Driven, Feasibility, Bypass                                                                               string
		StillWorks                                                                                                        string `json:"still_works"`
		ErrorCase                                                                                                         string `json:"error_case"`
		Kind                                                                                                              logKind
		Owns, Options, Labels                                                                                             []string
		Recommend, Line                                                                                                   int
		Leave                                                                                                             []int
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return turn.Result{}, fmt.Errorf("%s: arguments are not the expected shape: %w", t.verb, err)
	}
	t.board = t.spokenBoard()
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
			switch args.Kind {
			case logSay, "":
				if err = sayable(args.Text); err == nil {
					ticket, err = t.board.Comment(args.ID, args.Text, t.actor)
				}
			case logEvidence:
				ticket, err = t.board.Evidence(args.ID, args.Text, t.actor, args.Line)
			case logBefore:
				ticket, err = t.board.CaptureBefore(args.ID, args.Text, t.actor)
			default:
				err = fmt.Errorf("kind is %s, %s or %s, not %q", logSay, logEvidence, logBefore, args.Kind)
			}
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
			draft := boardy.Ticket{Front: boardy.Front{Title: args.Title, Owns: args.Owns, Labels: args.Labels, Parent: args.Parent, Epic: args.Epic, Sprint: args.Sprint}, Problem: args.Problem, Scope: args.Scope,
				Before: args.Before, Driven: args.Driven, StillWorks: args.StillWorks, ErrorCase: args.ErrorCase, Feasibility: args.Feasibility, Acceptance: []string{args.Acceptance}}
			file := t.board.Create
			if t.verb == ticketRequest {
				file = t.board.Request
			}
			if ticket, err = file(args.Board, draft, t.actor); ticket.ID == "" {
				said = ticket.Reason
			} else {
				said = "created " + ticket.ID + ", " + string(ticket.Status)
			}
		case ticketMove:
			var to boardy.Status
			before, _ := t.board.Get(args.ID)
			reason := strings.TrimSpace(args.Reason)
			switch to, err = boardy.ParseStatus(args.To); {
			case err != nil:
			case strings.Contains(reason, "\n") || sentencesIn(strings.Fields(reason)) > 1:
				err = fmt.Errorf("a move reason is one sentence on one line: say the rest with ticket_log kind %s, and put what you ran and what it printed in an entry of kind %s", logSay, logEvidence)
			case args.Why != "" && args.Bypass == "":
				err = errors.New("why goes with bypass, naming the hook it bypasses")
			default:
				moving := t.board
				moving.Bypass = boardy.Bypassing{Hook: args.Bypass, Why: args.Why}
				ticket, err = moving.MoveLeaving(args.ID, to, args.Reason, t.actor, args.Leave)
			}
			said = ticket.ID + " is " + string(ticket.Status)
			if before.Status != ticket.Status {
				said += ", was " + string(before.Status)
			}
		case ticketNotPossible:
			if ticket, err = t.board.ReportNotPossible(args.ID, boardy.Question{ELI5: args.Reason, Options: args.Options, Recommend: args.Recommend, Why: args.Why}, t.actor); err == nil {
				said = ticket.ID + " is " + string(ticket.Status) + ": the person decides it, so end your work here and say in your last message what cannot be done and why"
			}
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
